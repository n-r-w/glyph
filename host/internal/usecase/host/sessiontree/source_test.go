//go:build !integration

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

// TestReadySummaryUsesActualSource verifies a ready result commits independently of unused model state.
func TestReadySummaryUsesActualSource(t *testing.T) {
	t.Parallel()

	// Arrange missing and unsupported unused selections with explicit extension attribution.
	selections := []model.Selection{
		{},
		{Provider: "unavailable", Model: "unused", ReasoningChoice: "unsupported"},
	}
	for _, selection := range selections {
		t.Run(string(selection.Provider), func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			active := NewMockActiveSession(controller)
			models := NewMockModelRequester(controller)
			handlers := NewMockRuntime(controller)
			service := New(active, models, handlers)
			tree := navigationTree(t, time.Unix(1, 0).UTC())
			source := session.BranchSummarySource{
				ExtensionID: mo.Some("producer"),
				Model:       mo.None[session.BranchSummaryModelSource](),
			}
			handler := Handler{ExtensionID: "forwarder", HandlerID: "supply"}
			active.EXPECT().Tree().Return(tree)
			active.EXPECT().SessionID().Return("session")
			models.EXPECT().ActiveSelection().Return(selection)
			registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{handler})
			expectRequestHandler(handlers, handler, gomock.Any(), RequestHandlerAction{
				Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[HandlerNavigationRequest](),
				ResultAction: ResultActionReplace, Result: mo.Some(HandlerBranchSummaryResult{
					Summary: "ready", Source: source,
				}),
			}, nil)
			active.EXPECT().CommitNavigation(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, command CommitCommand) (session.Tree, error) {
					assert.Equal(t, source, command.BranchSummary.OrEmpty().Source)
					return tree, nil
				},
			)

			// Act with no availability, credential, or model-request expectation.
			result, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
				TargetEntryID: "user",
				SummaryMode:   sessionnavigation.SummaryModeSummarize,
				CustomFocus:   mo.None[string](),
			})

			// Assert the supplied result reaches commit without consulting the unused model.
			require.NoError(t, err)
			assert.False(t, result.Canceled)
		})
	}
}
