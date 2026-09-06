//go:build !integration

package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// commitNavigationForTest commits with an accepting client publisher.
func commitNavigationForTest(
	t *testing.T,
	service *Service,
	ctx context.Context,
	command sessiontree.CommitCommand,
) (sessiontree.NavigationCommit, error) {
	t.Helper()
	return service.CommitNavigation(ctx, command, func(session.Tree) error { return nil })
}

// TestCommitNavigationPersistsBeforePublishingAndContinuationUsesDestination verifies atomic branch-preserving
// navigation.
func TestCommitNavigationPersistsBeforePublishingAndContinuationUsesDestination(t *testing.T) {
	t.Parallel()

	// Arrange a branch whose active leaf will be abandoned, followed by one continuation append.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	createdAt := time.Unix(1, 0).UTC()
	tree := commitNavigationTree(t, createdAt)
	service := New(repository, ids, clock, pricing, "/project")
	service.active = commitNavigationLoadedSession(tree, createdAt)
	call := 0
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			call++
			if call == 1 {
				// The first persistence call contains one navigation-only mutation.
				navigation := command.Mutation.Navigation.MustGet()
				assert.Equal(t, mo.Some("destination"), navigation.DestinationID)
				assert.True(t, navigation.BranchSummary.IsNone())
				assert.True(t, command.Mutation.Entry.IsNone())
			} else {
				// The next durable entry continues from the committed destination.
				assert.Equal(t, mo.Some("destination"), command.Mutation.Entry.MustGet().ParentID)
			}
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	).Times(2)
	ids.EXPECT().NewID().Return("continuation", nil)
	clock.EXPECT().Now().Return(createdAt.Add(3 * time.Second))

	// Act by navigating and then appending the next user entry.
	committed, err := commitNavigationForTest(t, service, t.Context(), navigationCommit("abandoned", "destination"))
	require.NoError(t, err)
	require.Equal(t, mo.Some("destination"), committed.Tree.ActiveLeafID())
	err = service.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("continued")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert navigation commits first, continuation advances from it, and the abandoned branch remains stored.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("destination"), committed.Tree.ActiveLeafID())
	assert.Equal(t, mo.Some("continuation"), service.Tree().ActiveLeafID())
	assert.Equal(
		t,
		[]string{"root", "destination", "abandoned", "continuation"},
		treeBehaviorEntryIDs(service.Tree().Entries()),
	)
}

// TestOverlappingMessageAppendContinuesFromNavigationCommit verifies another extension waits for publication ordering.
func TestOverlappingMessageAppendContinuesFromNavigationCommit(t *testing.T) {
	t.Parallel()

	// Arrange a navigation publisher that admits another extension append while the session boundary remains held.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, ids, clock, nil, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	expected := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&expected)
	calls := 0
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			calls++
			if calls == 2 {
				assert.Equal(t, mo.Some("destination"), command.Mutation.Entry.MustGet().ParentID)
			}
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	).Times(2)
	ids.EXPECT().NewID().Return("overlap", nil)
	clock.EXPECT().Now().Return(createdAt.Add(3 * time.Second))
	entryPublisher := NewMockEntryPublisher(controller)
	service.BindEntryPublisher(entryPublisher)
	entryPublisher.EXPECT().PublishSessionEntry(gomock.Any()).Return(func(context.Context) error { return nil }, nil)
	appendResult := make(chan error, 1)
	publisher := func(session.Tree) error {
		go func() {
			_, appendErr := service.AppendExtensionMessage(
				t.Context(), expected, session.ExtensionMessage{
					ExtensionID: "other",
					EntryType:   "note",
					Text:        "overlap",
					Visibility:  session.ClientVisibilityVisible,
				}, treeBehaviorCommitGuard,
			)
			appendResult <- appendErr
		}()
		return nil
	}

	// Act by committing navigation while the independent append overlaps publication.
	_, err := service.CommitNavigation(t.Context(), navigationCommit("abandoned", "destination"), publisher)
	require.NoError(t, err)
	require.NoError(t, <-appendResult)

	// Assert the later append remains active and attaches to the committed navigation destination.
	tree := service.Tree()
	require.Equal(t, mo.Some("overlap"), tree.ActiveLeafID())
	entries := tree.Entries()
	require.Equal(t, mo.Some("destination"), entries[len(entries)-1].ParentID)
}

// TestCommitNavigationEnqueuesSnapshotInsidePublicationBoundary verifies persistence, state, and enqueue ordering.
func TestCommitNavigationEnqueuesSnapshotInsidePublicationBoundary(t *testing.T) {
	t.Parallel()

	// Arrange a navigation commit and a publisher that inspects the session lock and committed snapshot.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, nil, nil, nil, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(
		ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil,
	)
	published := false
	publisher := func(progress session.Tree) error {
		published = true
		if service.mutex.TryRLock() {
			service.mutex.RUnlock()
			t.Error("navigation publication ran outside the session commit boundary")
		}
		assert.Equal(t, mo.Some("destination"), progress.ActiveLeafID())
		assert.Equal(t, []string{"root", "destination"}, treeBehaviorEntryIDs(progress.ActiveBranch()))
		return nil
	}

	// Act by committing through the state owner with an operation progress publisher.
	commit, err := service.CommitNavigation(
		t.Context(), navigationCommit("abandoned", "destination"), publisher,
	)

	// Assert the committed snapshot was enqueued before the owner released its publication boundary.
	require.NoError(t, err)
	assert.True(t, published)
	assert.True(t, commit.Committed)
	assert.Equal(t, mo.Some("destination"), commit.Tree.ActiveLeafID())
}

// TestCommitNavigationPublicationFailureRetainsCommittedState verifies enqueue failure cannot roll back persistence.
func TestCommitNavigationPublicationFailureRetainsCommittedState(t *testing.T) {
	t.Parallel()

	// Arrange a durable navigation whose client progress enqueue fails.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, nil, nil, nil, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(
		ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil,
	)
	deliveryErr := errors.New("ordered writer queue is closed")

	publisher := func(session.Tree) error { return deliveryErr }

	// Act by committing with a publisher that cannot enqueue progress.
	commit, err := service.CommitNavigation(
		t.Context(), navigationCommit("abandoned", "destination"), publisher,
	)

	// Assert the caller receives the delivery cause with the committed snapshot still available.
	require.ErrorIs(t, err, deliveryErr)
	assert.True(t, commit.Committed)
	assert.Equal(t, mo.Some("destination"), commit.Tree.ActiveLeafID())
	assert.Equal(t, mo.Some("destination"), service.Tree().ActiveLeafID())
}

// TestCommitNavigationRejectsChangedActiveLeafWithoutPersistence verifies optimistic leaf comparison precedes storage.
func TestCommitNavigationRejectsChangedActiveLeafWithoutPersistence(t *testing.T) {
	t.Parallel()

	// Arrange an active tree whose current leaf differs from the expected snapshot.
	controller := gomock.NewController(t)
	service := New(
		NewMockRepository(controller), NewMockIDGenerator(controller), NewMockClock(controller),
		NewMockPricingCatalog(controller), "/project",
	)
	createdAt := time.Unix(1, 0).UTC()
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)

	// Act with a stale expected active leaf.
	_, err := commitNavigationForTest(t, service, t.Context(), navigationCommit("destination", "root"))

	// Assert no repository call occurs and the preceding active leaf remains published.
	require.Error(t, err)
	assert.Equal(t, mo.Some("abandoned"), service.Tree().ActiveLeafID())
}

// TestCommitNavigationCancellationWritesNothing verifies canceled navigation stops before storage.
func TestCommitNavigationCancellationWritesNothing(t *testing.T) {
	t.Parallel()

	// Arrange an active tree and an already canceled context with no repository expectation.
	controller := gomock.NewController(t)
	service := New(
		NewMockRepository(controller), NewMockIDGenerator(controller), NewMockClock(controller),
		NewMockPricingCatalog(controller), "/project",
	)
	createdAt := time.Unix(1, 0).UTC()
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act after cancellation.
	_, err := commitNavigationForTest(t, service, ctx, navigationCommit("abandoned", "destination"))

	// Assert cancellation is returned and the active leaf is unchanged.
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, mo.Some("abandoned"), service.Tree().ActiveLeafID())
}

// TestExtensionStateIsCoherentDuringNavigation verifies the leaf and filtered entries share one read lock.
func TestExtensionStateIsCoherentDuringNavigation(t *testing.T) {
	t.Parallel()

	// Arrange a navigation that abandons the caller extension's checkpoint while persistence is blocked.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	createdAt := time.Unix(1, 0).UTC()
	root := treeBehaviorUserEntry("root", mo.None[string](), createdAt)
	destination := treeBehaviorUserEntry("destination", mo.Some("root"), createdAt.Add(time.Second))
	checkpoint := treeBehaviorExtensionEntry(
		"checkpoint", mo.Some("destination"), createdAt.Add(2*time.Second), "caller",
	)
	tree, err := session.NewTree([]session.Entry{root, destination, checkpoint}, mo.Some("checkpoint"), nil)
	require.NoError(t, err)
	service := New(repository, nil, nil, nil, "/project")
	service.active = commitNavigationLoadedSession(tree, createdAt)
	expected := extensioncontext.SessionIdentity{
		ID: "session", WorkingDirectory: "/project", Incarnation: 1,
	}
	service.contextIdentity.Store(&expected)
	persistenceStarted := make(chan struct{})
	allowPersistence := make(chan struct{})
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, ApplyCommand) (ApplyResult, error) {
			close(persistenceStarted)
			<-allowPersistence
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	)
	navigationDone := make(chan error, 1)
	go func() {
		_, navigationErr := commitNavigationForTest(t, service,
			t.Context(), navigationCommit("checkpoint", "destination"),
		)
		navigationDone <- navigationErr
	}()
	<-persistenceStarted
	recoveryStarted := make(chan struct{})
	recoveryDone := make(chan navigationRecoveryResult, 1)
	go func() {
		close(recoveryStarted)
		snapshot, recoveryErr := service.ExtensionState(t.Context(), expected, "caller")
		recoveryDone <- navigationRecoveryResult{snapshot: snapshot, err: recoveryErr}
	}()
	<-recoveryStarted
	if service.mutex.TryRLock() {
		service.mutex.RUnlock()
		t.Fatal("recovery overlap did not encounter the navigation write boundary")
	}

	// Act by completing the overlapping navigation commit.
	close(allowPersistence)
	result := <-recoveryDone

	// Assert recovery contains the committed leaf and no entry from the abandoned branch.
	require.NoError(t, <-navigationDone)
	require.NoError(t, result.err)
	assert.Equal(t, session.ID("session"), result.snapshot.SessionID)
	assert.Equal(t, mo.Some("destination"), result.snapshot.ActiveLeafID)
	assert.Empty(t, result.snapshot.Entries)
}

// TestCommitNavigationPersistenceFailurePreservesPublishedTree verifies failed storage never publishes its candidate.
func TestCommitNavigationPersistenceFailurePreservesPublishedTree(t *testing.T) {
	t.Parallel()

	// Arrange a valid navigation whose single persistence mutation fails.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, ids, clock, pricing, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("sync failed"))

	// Act by committing navigation.
	_, err := commitNavigationForTest(t, service, t.Context(), navigationCommit("abandoned", "destination"))

	// Assert persistence failure preserves the preceding active leaf and all entries.
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
	assert.Equal(t, mo.Some("abandoned"), service.Tree().ActiveLeafID())
	assert.Equal(t, []string{"root", "destination", "abandoned"}, treeBehaviorEntryIDs(service.Tree().Entries()))
}

// navigationRecoveryResult contains one asynchronous snapshot read outcome.
type navigationRecoveryResult struct {
	// snapshot contains the coherent session read when successful.
	snapshot extensioncontext.SessionSnapshot
	// err contains the recovery failure when present.
	err error
}

// navigationCommit creates one no-summary optimistic commit command.
func navigationCommit(expected, destination string) sessiontree.CommitCommand {
	return sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some(expected), DestinationID: mo.Some(destination),
		BranchSummary: mo.None[sessiontree.BranchSummaryDraft](),
	}
}

// commitNavigationTree creates one active branch with an earlier navigation destination.
func commitNavigationTree(t *testing.T, createdAt time.Time) session.Tree {
	t.Helper()
	tree, err := session.NewTree([]session.Entry{
		treeBehaviorUserEntry("root", mo.None[string](), createdAt),
		treeBehaviorUserEntry("destination", mo.Some("root"), createdAt.Add(time.Second)),
		treeBehaviorUserEntry("abandoned", mo.Some("destination"), createdAt.Add(2*time.Second)),
	}, mo.Some("abandoned"), nil)
	require.NoError(t, err)
	return tree
}

// commitNavigationLoadedSession wraps a test tree in the active-session storage snapshot.
func commitNavigationLoadedSession(tree session.Tree, createdAt time.Time) LoadedSession {
	return LoadedSession{
		Header: session.Header{
			ID: "session", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/session.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
}
