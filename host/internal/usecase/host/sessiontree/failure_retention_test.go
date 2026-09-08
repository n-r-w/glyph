//go:build !integration

package sessiontree

import (
	"context"
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

// TestNavigationFailureRetainsEarlierHandlerIssues composes accumulated diagnostics with every later failure stage.
func TestNavigationFailureRetainsEarlierHandlerIssues(t *testing.T) {
	t.Parallel()
	for _, client := range []string{"ui", "programmatic"} {
		for _, stage := range []string{
			"summary", "validation", "precommit", "request cancellation", "result cancellation", "final cancellation",
		} {
			t.Run(client+"/"+stage, func(t *testing.T) {
				t.Parallel()
				// Arrange ordinary and invalid-action issues before a later navigation failure.
				controller := gomock.NewController(t)
				active := NewMockActiveSession(controller)
				models := NewMockModelRequester(controller)
				handlers := NewMockRuntime(controller)
				service := New(active, models, handlers)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cause := errors.New("earlier ordinary handler diagnostic Ω")
				later := errors.New("later navigation failure Ω")
				selection := model.Selection{
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: model.ReasoningChoiceOff,
				}
				active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()))
				active.EXPECT().SessionID().Return("session")
				models.EXPECT().ActiveSelection().Return(selection)
				failed := Handler{ExtensionID: "extension", HandlerID: "failed"}
				invalid := Handler{ExtensionID: "extension", HandlerID: "invalid"}
				last := Handler{ExtensionID: "extension", HandlerID: "last"}
				chain := []Handler{failed, invalid}
				if stage != "summary" && stage != "precommit" {
					chain = append(chain, last)
				}
				registerTestHandlers(service, handlers, HandlerKindRequest, chain)
				expectRequestHandler(handlers, failed, gomock.Any(), RequestHandlerAction{}, cause)
				expectRequestHandler(handlers, invalid, gomock.Any(), RequestHandlerAction{}, nil)
				mode := SummaryModeNoSummary
				expected := later
				var resultCause error
				switch stage {
				case "summary":
					mode = SummaryModeSummarize
					models.EXPECT().
						Request(gomock.Any(), selection, gomock.Any(), gomock.Any()).
						Return(model.Response{}, later)
					expected = ErrModelFailed
				case "precommit":
					active.EXPECT().
						CommitNavigation(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(NavigationCommit{}, later)
				case "request cancellation":
					expectRequestHandler(handlers, last, gomock.Any(), RequestHandlerAction{}, context.Canceled).
						Do(func(context.Context, string, string, HandlerRequest) { cancel() })
					expected = context.Canceled
				case "final cancellation":
					expectRequestHandler(handlers, last, gomock.Any(), RequestHandlerAction{
						Cancel:        false,
						RequestAction: RequestActionPreserve,
						Request:       mo.None[HandlerNavigationRequest](),
						ResultAction:  ResultActionPreserve,
						Result:        mo.None[HandlerBranchSummaryResult](),
					}, nil).Do(func(context.Context, string, string, HandlerRequest) { cancel() })
					expected = context.Canceled
				default:
					expectRequestHandler(handlers, last, gomock.Any(), RequestHandlerAction{
						Cancel:        false,
						RequestAction: RequestActionPreserve,
						Request:       mo.None[HandlerNavigationRequest](),
						ResultAction:  ResultActionReplace,
						Result: mo.Some(
							HandlerBranchSummaryResult{Summary: "ready", Source: session.BranchSummarySource{
								ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
							}},
						),
					}, nil)
					expected = ErrExtensionInvalidResult
					if stage == "result cancellation" {
						mode = SummaryModeSummarize
						resultCause = errors.New("earlier result handler diagnostic Ω")
						registerTestHandlers(service, handlers, HandlerKindResult, chain)
						expectResultHandler(handlers, failed, gomock.Any(), ResultHandlerAction{}, resultCause)
						expectResultHandler(handlers, invalid, gomock.Any(), ResultHandlerAction{}, nil)
						expectResultHandler(handlers, last, gomock.Any(), ResultHandlerAction{}, context.Canceled).
							Do(func(context.Context, string, string, HandlerRequest) { cancel() })
						expected = context.Canceled
					}
				}
				publish := func(session.Tree) error { t.Error("uncommitted navigation was published"); return nil }

				// Act through each actual client-facing navigation implementation.
				var err error
				if client == "ui" {
					_, err = service.NavigateUI(ctx, ui.NavigationIntent{
						TargetEntryID: "user",
						SummaryMode:   uicontroller.SummaryMode(mode),
						CustomFocus:   mo.None[string](),
					}, publish)
				} else {
					_, err = service.NavigateProgrammatic(ctx, programmatic.NavigationIntent{
						TargetEntryID: "user",
						SummaryMode:   programmaticcontroller.SummaryMode(mode),
						CustomFocus:   mo.None[string](),
					}, publish)
				}

				// Assert the primary failure and every prior diagnostic survive without commit or publication.
				require.ErrorIs(t, err, expected)
				if category, classified := errors.AsType[ui.NavigationFailure](expected); classified {
					actual, found := errors.AsType[ui.NavigationFailure](err)
					require.True(t, found)
					require.Equal(t, category.NavigationCode(), actual.NavigationCode())
				}
				require.ErrorContains(t, err, cause.Error())
				require.ErrorIs(t, err, cause)
				require.ErrorContains(t, err, invalidHandlerActionMessage)
				if resultCause != nil {
					require.ErrorIs(t, err, resultCause)
				}
			})
		}
	}
}
