package sessiontree

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestNavigateCommitsPreparedDestination verifies user and non-user targets select their defined destinations.
func TestNavigateCommitsPreparedDestination(t *testing.T) {
	t.Parallel()

	createdAt := time.Unix(1, 0).UTC()
	tests := []struct {
		name              string
		targetID          string
		expectedLeaf      mo.Option[string]
		expectedNextInput mo.Option[string]
	}{
		{
			name:              "non-root user",
			targetID:          "user",
			expectedLeaf:      mo.Some("root"),
			expectedNextInput: mo.Some("exact input"),
		},
		{
			name:              "root user",
			targetID:          "root",
			expectedLeaf:      mo.None[string](),
			expectedNextInput: mo.Some("root input"),
		},
		{
			name:              "non-user",
			targetID:          "extension",
			expectedLeaf:      mo.Some("extension"),
			expectedNextInput: mo.None[string](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one immutable tree snapshot and a commit that publishes the prepared destination.
			controller := gomock.NewController(t)
			active := NewMockActiveSession(controller)
			models := NewMockModelRequester(controller)
			handlers := NewMockHandlerRunner(controller)
			tree := navigationTree(t, createdAt)
			active.EXPECT().Tree().Return(tree)
			active.EXPECT().SessionID().Return("session")
			models.EXPECT().ActiveSelection().Return(model.Selection{})
			handlers.EXPECT().Handlers(HandlerKindRequest).Return(nil)
			committed := tree
			require.NoError(t, committed.SetActiveLeaf(test.expectedLeaf))
			active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
				ExpectedActiveLeafID: mo.Some("active"), DestinationID: test.expectedLeaf,
				BranchSummary: mo.None[BranchSummaryDraft](),
			}).Return(committed, nil)
			handlers.EXPECT().Handlers(HandlerKindObserver).Return(nil)
			service := New(active, models, handlers)

			// Act by navigating to the selected tree entry.
			result, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
				TargetEntryID: test.targetID, SummaryMode: sessionnavigation.SummaryModeNoSummary,
				CustomFocus: mo.None[string](),
			})

			// Assert the committed leaf and editable user text match the prepared target semantics.
			require.NoError(t, err)
			assert.Equal(t, sessionnavigation.Result{
				Canceled: false, Tree: committed, ActiveLeafID: test.expectedLeaf,
				ActiveBranch: committed.ActiveBranch(), NextInput: test.expectedNextInput, Issues: nil,
			}, result)
		})
	}
}

// TestNavigateRejectsUnknownTargetWithoutCommit verifies target validation precedes active-session mutation.
func TestNavigateRejectsUnknownTargetWithoutCommit(t *testing.T) {
	t.Parallel()

	// Arrange a tree snapshot with no matching target and no commit expectation.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockHandlerRunner(controller)
	active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()))
	service := New(active, models, handlers)

	// Act by selecting an unknown entry.
	_, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "unknown", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	})

	// Assert preparation fails with the stable target classification before commit.
	require.ErrorIs(t, err, session.ErrEntryNotFound)
}

// TestNavigateHonorsCanceledContextBeforeReadingTree verifies cancellation performs no session work.
func TestNavigateHonorsCanceledContextBeforeReadingTree(t *testing.T) {
	t.Parallel()

	// Arrange an already canceled request and an active session with no expectations.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockHandlerRunner(controller)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service := New(active, models, handlers)

	// Act after cancellation.
	_, err := service.NavigateTree(ctx, sessionnavigation.Request{
		TargetEntryID: "root", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	})

	// Assert cancellation is returned without reading or committing session state.
	require.ErrorIs(t, err, context.Canceled)
}

// navigationTree creates a branched tree used by no-summary navigation tests.
func navigationTree(t *testing.T, createdAt time.Time) session.Tree {
	t.Helper()
	entries := []session.Entry{
		navigationUserEntry("root", mo.None[string](), "root input", createdAt),
		navigationUserEntry("user", mo.Some("root"), "exact input", createdAt.Add(time.Second)),
		{
			ID:            "extension",
			ParentID:      mo.Some("user"),
			CreatedAt:     createdAt.Add(2 * time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension: mo.Some(
				session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "state", Data: []byte(`{}`)},
			),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		navigationUserEntry("active", mo.Some("extension"), "active input", createdAt.Add(3*time.Second)),
	}
	tree, err := session.NewTree(entries, mo.Some("active"), nil)
	require.NoError(t, err)
	return tree
}

// navigationUserEntry creates one valid user-message tree entry with exact text.
func navigationUserEntry(id string, parentID mo.Option[string], text string, createdAt time.Time) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage(text)),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}
