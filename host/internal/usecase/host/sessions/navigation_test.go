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
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestCommitNavigationPersistsBeforePublishingAndContinuationUsesDestination verifies atomic branch-preserving navigation.
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
	committed, err := service.CommitNavigation(t.Context(), navigationCommit("abandoned", "destination"))
	require.NoError(t, err)
	require.Equal(t, mo.Some("destination"), committed.ActiveLeafID())
	err = service.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("continued")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert navigation commits first, continuation advances from it, and the abandoned branch remains stored.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("destination"), committed.ActiveLeafID())
	assert.Equal(t, mo.Some("continuation"), service.Tree().ActiveLeafID())
	assert.Equal(
		t,
		[]string{"root", "destination", "abandoned", "continuation"},
		treeBehaviorEntryIDs(service.Tree().Entries()),
	)
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
	_, err := service.CommitNavigation(t.Context(), navigationCommit("destination", "root"))

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
	_, err := service.CommitNavigation(ctx, navigationCommit("abandoned", "destination"))

	// Assert cancellation is returned and the active leaf is unchanged.
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, mo.Some("abandoned"), service.Tree().ActiveLeafID())
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
	_, err := service.CommitNavigation(t.Context(), navigationCommit("abandoned", "destination"))

	// Assert persistence failure preserves the preceding active leaf and all entries.
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
	assert.Equal(t, mo.Some("abandoned"), service.Tree().ActiveLeafID())
	assert.Equal(t, []string{"root", "destination", "abandoned"}, treeBehaviorEntryIDs(service.Tree().Entries()))
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
			Version: formatVersion, ID: "session", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/session.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
}
