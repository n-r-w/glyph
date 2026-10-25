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

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// TestNavigationParticipantFailureStopsChain verifies failures prevent later participation and retain actual commits.
func TestNavigationParticipantFailureStopsChain(t *testing.T) {
	t.Parallel()
	for _, kind := range []HandlerKind{HandlerKindRequest, HandlerKindResult, HandlerKindObserver} {
		for _, mode := range []string{"error", "invalid", "malformed", "mixed", "owning", "context"} {
			if kind == HandlerKindObserver && mode == "invalid" {
				continue
			}
			t.Run(
				map[HandlerKind]string{HandlerKindRequest: "request", HandlerKindResult: "result", HandlerKindObserver: "observer"}[kind]+"/"+mode,
				func(t *testing.T) {
					t.Parallel()
					// Arrange a selected failing participant followed by an uncalled participant.
					control := gomock.NewController(t)
					active := NewMockActiveSession(control)
					runtime := NewMockRuntime(control)
					selection := NewMockModelSelection(control)
					service := New(active, selection, NewMockModelRequester(control), runtime)
					tree := navigationTree(t, time.Unix(1, 0).UTC())
					active.EXPECT().Tree().Return(tree, nil)
					active.EXPECT().SessionID().Return("session")
					selection.EXPECT().ActiveSelection().Return(model.Selection{})
					first := Handler{ExtensionID: "extension", HandlerID: "first"}
					later := Handler{ExtensionID: "extension", HandlerID: "later"}
					registerTestHandlers(service, runtime, kind, []Handler{first, later})
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					source := errors.New("navigation participant complete original cause")
					failure := source
					if mode == "mixed" {
						failure = errors.Join(source, context.Canceled)
					}
					if mode == "owning" {
						failure = context.Canceled
					}
					if mode == "invalid" || mode == "malformed" {
						failure = nil
					}
					response := HandlerResponse{}
					switch kind {
					case HandlerKindRequest:
						response = HandlerResponse{
							Request:  mo.Some(RequestHandlerAction{}),
							Result:   mo.None[ResultHandlerAction](),
							Observer: mo.None[ObserverAction](),
						}
					case HandlerKindResult:
						response = HandlerResponse{
							Request:  mo.None[RequestHandlerAction](),
							Result:   mo.Some(ResultHandlerAction{}),
							Observer: mo.None[ObserverAction](),
						}
						request := Handler{ExtensionID: "extension", HandlerID: "ready"}
						registerTestHandlers(service, runtime, HandlerKindRequest, []Handler{request})
						expectRequestHandler(runtime, request, gomock.Any(), RequestHandlerAction{
							Cancel:        false,
							RequestAction: RequestActionPreserve,
							Request:       mo.None[HandlerNavigationRequest](),
							ResultAction:  ResultActionReplace,
							Result: mo.Some(HandlerBranchSummaryResult{
								Summary: "ready",
								Source: session.BranchSummarySource{
									ExtensionID: mo.Some("extension"),
									Model:       mo.None[session.BranchSummaryModelSource](),
								},
							}),
						}, nil)
					case HandlerKindObserver:
						response = HandlerResponse{
							Request:  mo.None[RequestHandlerAction](),
							Result:   mo.None[ResultHandlerAction](),
							Observer: mo.Some(ObserverAction{}),
						}
						committed, err := tree.Clone()
						require.NoError(t, err)
						require.NoError(t, committed.SetActiveLeaf(mo.None[string]()))
						active.EXPECT().
							CommitNavigation(gomock.Any(), gomock.Any(), gomock.Any()).
							Return(NavigationCommit{
								Committed: true, Tree: committed, CreatedSummary: mo.None[session.Entry](),
							}, nil)
					}
					if mode == "malformed" {
						response = HandlerResponse{}
					}
					if mode == "context" {
						contexts := NewMockContextIssuer(control)
						if kind == HandlerKindResult {
							gomock.InOrder(
								contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil),
								contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, source),
							)
						} else {
							contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, source)
						}
						service.BindContextIssuer(contexts)
					} else {
						runtime.EXPECT().
							HandleHandler(gomock.Any(), first.ExtensionID, first.HandlerID, gomock.Any()).
							DoAndReturn(
								func(context.Context, string, string, HandlerRequest) (HandlerResponse, error) {
									if mode == "mixed" || mode == "owning" {
										cancel()
									}
									return response, failure
								})
					}
					summaryMode := controllerui.SummaryModeNoSummary
					if kind == HandlerKindResult {
						summaryMode = controllerui.SummaryModeSummarize
					}

					// Act through the client navigation entry point with a root destination.
					result, err := service.NavigateUI(ctx, ui.NavigationIntent{
						TargetEntryID: "root", SummaryMode: summaryMode, CustomFocus: mo.None[string](),
					}, func(session.Tree) error { return nil }, nil)

					// Assert no later handler or uncommitted persistence is reached and causes remain available.
					require.Error(t, err)
					if mode == "error" || mode == "mixed" || mode == "context" {
						require.ErrorIs(t, err, source)
						require.ErrorContains(t, err, source.Error())
					}
					if mode == "owning" {
						require.ErrorIs(t, err, context.Canceled)
					}
					require.Equal(t, kind == HandlerKindObserver, result.Committed.IsSome())
					if committed, present := result.Committed.Get(); present {
						require.True(t, committed.DestinationID.IsNone())
						require.True(t, committed.ActiveLeafID.IsNone())
						require.Equal(t, mo.Some("root input"), committed.NextInput)
					}
				},
			)
		}
	}
}
