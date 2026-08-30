package sessiontree

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestNavigateSummarizesOnlyAbandonedPath verifies built-in and custom modes use one snapshotted selection and exact branch boundary.
func TestNavigateSummarizesOnlyAbandonedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		mode  sessionnavigation.SummaryMode
		focus mo.Option[string]
	}{
		{name: "built in", mode: sessionnavigation.SummaryModeSummarize, focus: mo.None[string]()},
		{name: "custom focus", mode: sessionnavigation.SummaryModeSummarizeWithCustomPrompt, focus: mo.Some("focus on tests")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a branch whose root remains at the destination and three later entries are abandoned.
			controller := gomock.NewController(t)
			active := NewMockActiveSession(controller)
			models := NewMockModelCompleter(controller)
			handlers := NewMockHandlerRunner(controller)
			tree := navigationTree(t, time.Unix(1, 0).UTC())
			selection := model.Selection{Provider: "provider", Model: "summary-model", ReasoningChoice: model.ReasoningChoiceHigh}
			active.EXPECT().Tree().Return(tree)
			active.EXPECT().SessionID().Return("session")
			models.EXPECT().Selection().Return(selection)
			handlers.EXPECT().Handlers(HandlerKindRequest).Return(nil)
			models.EXPECT().CompleteConfigured(gomock.Any(), selection, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ model.Selection, _ string, history []agent.HistoryEntry) (model.Response, error) {
					require.Len(t, history, 2)
					assert.Equal(t, agent.HistoryEntryUser, history[0].Kind)
					assert.Equal(t, "exact input", history[0].User.OrEmpty().Text("\n"))
					assert.Equal(t, agent.HistoryEntryUser, history[1].Kind)
					assert.Equal(t, "active input", history[1].User.OrEmpty().Text("\n"))
					return summaryResponse("generated summary", mo.Some(model.Usage{
						InputTokens: 5, OutputTokens: 5, CachedInputTokens: 3,
						CacheWriteTokens: 2, ReasoningTokens: 2, TotalTokens: 15,
					})), nil
				},
			)
			expectedUsage := session.TokenUsage{
				InputTokens: 5, OutputTokens: 5, CacheReadTokens: 3,
				CacheWriteTokens: 2, ReasoningTokens: 2, TotalTokens: 15,
			}
			handlers.EXPECT().Handlers(HandlerKindResult).Return(nil)
			models.EXPECT().ValidateConfigured(gomock.Any(), selection).Return(nil)
			active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
				ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("root"),
				BranchSummary: mo.Some(BranchSummaryDraft{
					Summary: "generated summary", FirstEntryID: "user", LastEntryID: "active",
					CommonAncestorID: mo.Some("root"), Selection: selection, Usage: mo.Some(expectedUsage),
				}),
			}).Return(tree, nil)
			handlers.EXPECT().Handlers(HandlerKindObserver).Return(nil)
			service := New(active, models, handlers)

			// Act by navigating to the earlier user message with summarization enabled.
			_, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
				TargetEntryID: "user", SummaryMode: test.mode, CustomFocus: test.focus,
			})

			// Assert the generated summary reaches the single atomic navigation commit.
			require.NoError(t, err)
		})
	}
}

// TestNavigateRejectsInvalidSummaryResponseWithoutCommit verifies invalid model output cannot mutate active state.
func TestNavigateRejectsInvalidSummaryResponseWithoutCommit(t *testing.T) {
	t.Parallel()

	// Arrange a valid abandoned path and a model response with no visible summary text.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelCompleter(controller)
	handlers := NewMockHandlerRunner(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()))
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().Selection().Return(selection)
	handlers.EXPECT().Handlers(HandlerKindRequest).Return(nil)
	models.EXPECT().CompleteConfigured(gomock.Any(), selection, gomock.Any(), gomock.Any()).Return(
		summaryResponse("   ", mo.None[model.Usage]()), nil,
	)
	service := New(active, models, handlers)

	// Act with built-in summarization.
	_, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeSummarize,
		CustomFocus: mo.None[string](),
	})

	// Assert model failure is classified and no commit is attempted.
	require.ErrorIs(t, err, sessionnavigation.ErrModelFailed)
}

// summaryResponse creates one terminal text response for summarizer tests.
func summaryResponse(text string, usage mo.Option[model.Usage]) model.Response {
	return model.Response{
		Content: []model.Content{{Kind: model.ContentText, Text: mo.Some(text), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: usage, Diagnostics: nil,
	}
}
