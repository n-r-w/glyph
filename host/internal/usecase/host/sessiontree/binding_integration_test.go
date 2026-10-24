//go:build integration

package sessiontree

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// TestNavigateRetainsModelsAfterRejectedBinding verifies navigation uses the original model owners
// after replacement fails.
func TestNavigateRetainsModelsAfterRejectedBinding(t *testing.T) {
	t.Parallel()
	// Arrange summary navigation using originally bound model owners and a real tree.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	selectionOwner := NewMockModelSelection(controller)
	requester := NewMockModelRequester(controller)
	service := New(active, nil, nil, NewMockRuntime(controller))
	require.NoError(t, service.BindModels(selectionOwner, requester))
	selection := model.Selection{
		Provider: "original-provider", Model: "original-summary-model", ReasoningChoice: model.ReasoningChoiceHigh,
	}
	entries := make([]session.Entry, 0, 3)
	parent := mo.None[string]()
	for _, id := range []string{"root", "target", "active"} {
		entries = append(entries, session.Entry{
			ID: id, ParentID: parent, CreatedAt: time.Unix(1, 0).UTC(),
			Information: mo.None[session.Information](), User: mo.Some(model.TextMessage(id)),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
			Compaction: mo.None[session.CompactionEntry](),
		})
		parent = mo.Some(id)
	}
	tree, err := session.NewTree(entries, mo.Some("active"), nil)
	require.NoError(t, err)
	active.EXPECT().Tree().Return(tree, nil)
	active.EXPECT().SessionID().Return("session")
	selectionOwner.EXPECT().ActiveSelection().Return(selection)
	requester.EXPECT().RequestConfigured(gomock.Any(), selection, gomock.Any(), gomock.Any(), gomock.Any()).Return(
		model.Response{
			Content: []model.Content{{
				Kind: model.ContentText, Text: mo.Some("retained owner summary"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			}},
			Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		}, nil,
	)
	active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
		ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("root"),
		BranchSummary: mo.Some(BranchSummaryDraft{
			Summary: "retained owner summary", FirstEntryID: "target", LastEntryID: "active",
			CommonAncestorID: mo.Some("root"), Source: session.BranchSummarySource{
				ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
					Selection: selection, Usage: mo.None[session.TokenUsage](),
				}),
			},
		}),
	}, gomock.Any()).Return(NavigationCommit{
		Committed: true, Tree: tree, CreatedSummary: mo.None[session.Entry](),
	}, nil)

	// Act by rejecting a replacement and navigating through the public UI entry point.
	require.Error(t, service.BindModels(NewMockModelSelection(controller), NewMockModelRequester(controller)))
	result, err := service.NavigateUI(t.Context(), ui.NavigationIntent{
		TargetEntryID: "target", SummaryMode: controllerui.SummaryModeSummarize, CustomFocus: mo.None[string](),
	}, func(session.Tree) error { return nil }, nil)

	// Assert the original selection/requester produce the summary commit and a successful navigation result.
	require.NoError(t, err)
	require.True(t, result.Committed.IsSome())
	require.Empty(t, result.Issues)
}
