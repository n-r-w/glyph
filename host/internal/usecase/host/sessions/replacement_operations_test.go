package sessions

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestForkActivePersistsOnlyTheSelectedUserParentPath verifies fork retention, identity, labels, and publication order.
func TestForkActivePersistsOnlyTheSelectedUserParentPath(t *testing.T) {
	t.Parallel()

	// Arrange a source tree whose retained path contains opaque data and unresolved summary provenance.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	createdAt := time.Unix(100, 0).UTC()
	ids.EXPECT().NewID().Return("forked", nil)
	clock.EXPECT().Now().Return(createdAt)
	entries := replacementEntries()
	tree := replacementTree(t, entries, "target", map[string]string{"summary": "kept", "target": "dropped"})
	service := New(repository, ids, clock, pricing, "/project")
	service.active = replacementLoadedSession(tree)
	source := service.active.Clone()
	repository.EXPECT().CreateSnapshot(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ any, command CreateSnapshotCommand) (CreateSnapshotResult, error) {
			require.Equal(t, session.ID("forked"), command.Header.ID)
			require.Equal(t, createdAt, command.Header.CreatedAt)
			require.Equal(t, []string{"root", "extension", "summary"}, lo.Map(command.Tree.Entries(), func(entry session.Entry, _ int) string {
				return entry.ID
			}))
			require.Equal(t, mo.Some("summary"), command.Tree.ActiveLeafID())
			require.Equal(t, map[string]string{"summary": "kept"}, command.Tree.Labels())
			require.Equal(t, mo.Some(session.Information{Name: "source name"}), command.Information)
			require.Equal(t, []byte("opaque"), command.Tree.Entries()[1].Extension.MustGet().Data)
			summary := command.Tree.Entries()[2].BranchSummary.MustGet()
			require.Equal(t, "outside-first", summary.FirstEntryID)
			require.Equal(t, "outside-last", summary.LastEntryID)
			return CreateSnapshotResult{StoragePath: "/sessions/forked.jsonl"}, nil
		},
	)

	// Act by forking before the selected user entry.
	replacement, nextInput, err := service.ForkActive(t.Context(), "target")

	// Assert the selected text and persisted replacement become active without changing the source snapshot.
	require.NoError(t, err)
	require.Equal(t, "exact target text", nextInput)
	require.Equal(t, session.ID("forked"), replacement.Info.ID)
	require.Equal(t, []string{"root", "extension", "summary"}, lo.Map(replacement.Entries, func(entry session.Entry, _ int) string {
		return entry.ID
	}))
	require.Equal(t, source.Tree.Entries(), tree.Entries())
	require.Equal(t, source.Tree.Labels(), tree.Labels())
	require.Equal(t, "/sessions/forked.jsonl", service.active.StoragePath)
}

// TestForkActiveRootUserCreatesEmptyReplacement verifies a root user target returns text with no retained leaf.
func TestForkActiveRootUserCreatesEmptyReplacement(t *testing.T) {
	t.Parallel()

	// Arrange one root user entry and a complete replacement snapshot expectation.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	ids.EXPECT().NewID().Return("forked", nil)
	clock.EXPECT().Now().Return(time.Unix(100, 0).UTC())
	tree := replacementTree(t, []session.Entry{replacementUserEntry("root", mo.None[string](), "root text")}, "root", nil)
	service := New(repository, ids, clock, pricing, "/project")
	service.active = replacementLoadedSession(tree)
	repository.EXPECT().CreateSnapshot(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ any, command CreateSnapshotCommand) (CreateSnapshotResult, error) {
			require.Empty(t, command.Tree.Entries())
			require.True(t, command.Tree.ActiveLeafID().IsNone())
			return CreateSnapshotResult{StoragePath: "/sessions/forked.jsonl"}, nil
		},
	)

	// Act by selecting the root user message.
	_, nextInput, err := service.ForkActive(t.Context(), "root")

	// Assert exact input is returned and the replacement remains at the implicit root.
	require.NoError(t, err)
	require.Equal(t, "root text", nextInput)
	require.Empty(t, service.active.Tree.Entries())
	require.True(t, service.active.Tree.ActiveLeafID().IsNone())
}

// TestForkActiveRejectsInvalidTargetsWithoutReplacement verifies lookup and user-kind validation precede creation.
func TestForkActiveRejectsInvalidTargetsWithoutReplacement(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		target   string
		expected error
	}{
		{name: "missing", target: "missing", expected: session.ErrEntryNotFound},
		{name: "non-user", target: "summary", expected: session.ErrInvalidForkTarget},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange strict dependencies with no creation or persistence calls expected.
			controller := gomock.NewController(t)
			repository := NewMockRepository(controller)
			service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), NewMockPricingCatalog(controller), "/project")
			service.active = replacementLoadedSession(replacementTree(t, replacementEntries(), "target", nil))
			before := service.active.Clone()

			// Act by selecting an invalid fork target.
			_, _, err := service.ForkActive(t.Context(), test.target)

			// Assert no active state changed and the stable domain error is returned.
			require.ErrorIs(t, err, test.expected)
			require.Equal(t, before.Tree.Entries(), service.active.Tree.Entries())
			require.Equal(t, before.Header, service.active.Header)
		})
	}
}

// TestCloneActivePersistsTheCompleteActiveBranch verifies clone retention and empty-session behavior.
func TestCloneActivePersistsTheCompleteActiveBranch(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		tree         func(*testing.T) session.Tree
		expectedIDs  []string
		expectedLeaf mo.Option[string]
	}{
		{name: "active branch", tree: func(t *testing.T) session.Tree {
			return replacementTree(t, replacementEntries(), "target", map[string]string{"summary": "kept", "target": "kept"})
		}, expectedIDs: []string{"root", "extension", "summary", "target"}, expectedLeaf: mo.Some("target")},
		{name: "empty", tree: func(t *testing.T) session.Tree {
			tree, err := session.NewTree(nil, mo.None[string](), nil)
			require.NoError(t, err)
			return tree
		}, expectedIDs: []string{}, expectedLeaf: mo.None[string]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a source and strict replacement persistence expectation.
			controller := gomock.NewController(t)
			repository := NewMockRepository(controller)
			ids := NewMockIDGenerator(controller)
			clock := NewMockClock(controller)
			ids.EXPECT().NewID().Return("clone", nil)
			clock.EXPECT().Now().Return(time.Unix(100, 0).UTC())
			service := New(repository, ids, clock, NewMockPricingCatalog(controller), "/project")
			service.active = replacementLoadedSession(test.tree(t))
			repository.EXPECT().CreateSnapshot(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ any, command CreateSnapshotCommand) (CreateSnapshotResult, error) {
					require.Equal(t, test.expectedIDs, lo.Map(command.Tree.Entries(), func(entry session.Entry, _ int) string {
						return entry.ID
					}))
					require.Equal(t, test.expectedLeaf, command.Tree.ActiveLeafID())
					return CreateSnapshotResult{StoragePath: "/sessions/clone.jsonl"}, nil
				},
			)

			// Act by cloning the complete active branch.
			replacement, err := service.CloneActive(t.Context())

			// Assert the exact retained branch is published.
			require.NoError(t, err)
			require.Equal(t, test.expectedIDs, lo.Map(replacement.Entries, func(entry session.Entry, _ int) string {
				return entry.ID
			}))
			require.Equal(t, session.ID("clone"), replacement.Info.ID)
		})
	}
}

// TestReplacementCreationFailurePreservesSource verifies durable creation precedes active publication.
func TestReplacementCreationFailurePreservesSource(t *testing.T) {
	t.Parallel()

	// Arrange clone creation that fails after identity allocation.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	ids.EXPECT().NewID().Return("clone", nil)
	clock.EXPECT().Now().Return(time.Unix(100, 0).UTC())
	repository.EXPECT().CreateSnapshot(gomock.Any(), gomock.Any()).Return(CreateSnapshotResult{}, errors.New("sync failed"))
	service := New(repository, ids, clock, NewMockPricingCatalog(controller), "/project")
	service.active = replacementLoadedSession(replacementTree(t, replacementEntries(), "target", nil))
	service.history = sessiontree.HistoryFromEntries(service.active.Tree.ActiveBranch())
	before := service.active.Clone()
	beforeHistory := append([]agent.HistoryEntry(nil), service.history...)

	// Act by cloning when persistence fails.
	_, err := service.CloneActive(t.Context())

	// Assert the source identity, tree, history, and write state remain unchanged.
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
	require.Equal(t, before.Header, service.active.Header)
	require.Equal(t, before.Tree.Entries(), service.active.Tree.Entries())
	require.Equal(t, before.Tree.ActiveLeafID(), service.active.Tree.ActiveLeafID())
	require.Equal(t, beforeHistory, service.history)
}

// TestSetLabelPublishesOnlyAfterPersistence verifies setting, clearing, missing-target, and failure behavior.
func TestSetLabelPublishesOnlyAfterPersistence(t *testing.T) {
	t.Parallel()

	// Arrange one labeled tree and ordered successful and failed repository mutations.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), NewMockPricingCatalog(controller), "/project")
	service.active = replacementLoadedSession(replacementTree(t, replacementEntries(), "target", nil))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, command ApplyCommand) (ApplyResult, error) {
		require.Equal(t, LabelMutation{TargetID: "target", Label: "branch"}, command.Mutation.Label.MustGet())
		return ApplyResult{StoragePath: "/sessions/source.jsonl"}, nil
	})
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("sync failed"))

	// Act by setting a label, attempting a failed clear, and targeting a missing entry.
	tree, err := service.SetLabel(t.Context(), "target", "branch")
	require.NoError(t, err)
	_, clearErr := service.SetLabel(t.Context(), "target", "")
	_, missingErr := service.SetLabel(t.Context(), "missing", "label")

	// Assert only the durable mutation is visible.
	require.Equal(t, map[string]string{"target": "branch"}, tree.Labels())
	require.ErrorIs(t, clearErr, session.ErrPersistenceUnavailable)
	require.ErrorIs(t, missingErr, session.ErrEntryNotFound)
	require.Equal(t, map[string]string{"target": "branch"}, service.active.Tree.Labels())
}

// replacementLoadedSession creates one active source with retained session information.
func replacementLoadedSession(tree session.Tree) LoadedSession {
	createdAt := time.Unix(1, 0).UTC()
	return LoadedSession{
		Header:      session.Header{Version: formatVersion, ID: "source", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/source.jsonl", Tree: tree,
		Information: mo.Some(session.Information{Name: "source name"}), InformationUpdatedAt: mo.Some(createdAt.Add(time.Minute)),
	}
}

// replacementEntries creates one active path with opaque data, summary provenance, and a selected user message.
func replacementEntries() []session.Entry {
	createdAt := time.Unix(1, 0).UTC()
	return []session.Entry{
		replacementUserEntry("root", mo.None[string](), "root"),
		{
			ID: "extension", ParentID: mo.Some("root"), CreatedAt: createdAt.Add(time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[session.ToolResult](),
			Extension:     mo.Some(session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "opaque", Data: []byte("opaque")}),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID: "summary", ParentID: mo.Some("extension"), CreatedAt: createdAt.Add(2 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.Some(session.BranchSummaryEntry{
				Summary: "summary", FirstEntryID: "outside-first", LastEntryID: "outside-last",
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow,
				Usage: mo.None[session.TokenUsage](), EstimatedCost: mo.None[session.EstimatedCost](),
			}),
		},
		replacementUserEntry("target", mo.Some("summary"), "exact target text"),
	}
}

// replacementUserEntry creates one fully initialized user tree entry.
func replacementUserEntry(id string, parent mo.Option[string], text string) session.Entry {
	return session.Entry{
		ID: id, ParentID: parent, CreatedAt: time.Unix(1, 0).UTC(), Information: mo.None[session.Information](),
		User: mo.Some(model.TextMessage(text)), Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// replacementTree creates one validated test tree with explicit labels and leaf.
func replacementTree(t *testing.T, entries []session.Entry, leaf string, labels map[string]string) session.Tree {
	t.Helper()
	tree, err := session.NewTree(entries, mo.Some(leaf), labels)
	require.NoError(t, err)
	return tree
}
