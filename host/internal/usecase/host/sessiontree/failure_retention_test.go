//go:build !integration

package sessiontree

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticcontroller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	uicontroller "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// TestNavigationFailureRetainsEarlierHandlerIssues verifies both client boundaries retain the first complete cause.
func TestNavigationFailureRetainsEarlierHandlerIssues(t *testing.T) {
	t.Parallel()
	for _, client := range []string{"ui", "programmatic"} {
		t.Run(client, func(t *testing.T) {
			t.Parallel()
			// Arrange a first failure and strict dependencies that prevent later work.
			control := gomock.NewController(t)
			active := NewMockActiveSession(control)
			runtime := NewMockRuntime(control)
			selection := NewMockModelSelection(control)
			service := New(active, selection, NewMockModelRequester(control), runtime)
			active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()), nil)
			active.EXPECT().SessionID().Return("session")
			selection.EXPECT().ActiveSelection().Return(model.Selection{})
			first := Handler{ExtensionID: "extension", HandlerID: "first"}
			later := Handler{ExtensionID: "extension", HandlerID: "later"}
			registerTestHandlers(service, runtime, HandlerKindRequest, []Handler{first, later})
			source := errors.New("first navigation complete original cause Ω")
			expectRequestHandler(runtime, first, gomock.Any(), RequestHandlerAction{}, source)
			publish := func(session.Tree) error { t.Error("uncommitted navigation published"); return nil }

			// Act through each client-facing navigation boundary.
			var err error
			if client == "ui" {
				result, failure := service.NavigateUI(
					t.Context(),
					ui.NavigationIntent{
						TargetEntryID: "user",
						SummaryMode:   uicontroller.SummaryModeSummarize,
						CustomFocus:   mo.None[string](),
					},
					publish,
					nil,
				)
				err = failure
				require.True(t, result.Committed.IsNone())
			} else {
				result, failure := service.NavigateProgrammatic(
					t.Context(),
					programmatic.NavigationIntent{
						TargetEntryID: "user",
						SummaryMode:   programmaticcontroller.SummaryModeSummarize,
						CustomFocus:   mo.None[string](),
					},
					publish,
					nil,
				)
				err = failure
				require.True(t, result.Committed.IsNone())
			}

			// Assert original cause identity and complete text survive without later handlers or model generation.
			require.ErrorIs(t, err, source)
			require.ErrorContains(t, err, source.Error())
		})
	}
}
