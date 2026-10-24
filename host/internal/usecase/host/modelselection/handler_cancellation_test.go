//go:build !integration

package modelselection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/extension"
	domainextension "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestSelectionHandlerCancellationPrecedence verifies independent participant failures win owning cancellation for both selection kinds.
func TestSelectionHandlerCancellationPrecedence(t *testing.T) {
	t.Parallel()
	for _, kind := range []HandlerKind{HandlerKindModel, HandlerKindReasoning} {
		name := "model"
		if kind == HandlerKindReasoning {
			name = "reasoning"
		}
		for _, mode := range []string{"cancel", "mixed", "invalid", "unavailable"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				// Arrange two selected handlers and cancellation while the first handler returns.
				controller := gomock.NewController(t)
				catalog := NewMockCatalog(controller)
				runtime := NewMockRuntime(controller)
				contexts := NewMockContextIssuer(controller)
				delivery := NewMockIssueDelivery(controller)
				service := New(catalog, NewMockPublisher(controller))
				service.BindHandlers(runtime, contexts, delivery)
				target := model.Selection{
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: model.ReasoningChoiceHigh,
				}
				rawKind := startup.RawHandlerKindModelSelection
				if kind == HandlerKindReasoning {
					rawKind = startup.RawHandlerKindReasoningSelection
					catalog.EXPECT().ResolveReasoning(target.ReasoningChoice).Return(target, nil)
				} else {
					catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
				}
				service.CommitSelectionHandlers([]startup.AcceptedRegistration{{
					ID: "extension", Path: "/extension", Tools: nil,
					Handlers: []startup.AcceptedHandler{{ID: "first", Kind: rawKind}, {ID: "later", Kind: rawKind}},
				}})
				runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
				contexts.EXPECT().IssueContext("extension").Return(domainextension.Context{}, nil)
				ctx, cancel := context.WithCancel(t.Context())
				cause := errors.New("independent selection failure: full cause")
				runtime.EXPECT().HandleSelection(gomock.Any(), "extension", "first", gomock.Any()).DoAndReturn(
					func(context.Context, string, string, HandlerInvocation) (HandlerAction, bool, error) {
						cancel()
						switch mode {
						case "cancel":
							return HandlerAction{}, false, context.Canceled
						case "mixed":
							return HandlerAction{}, false, errors.Join(cause, context.Canceled)
						case "unavailable":
							return HandlerAction{}, true, cause
						default:
							return HandlerAction{}, false, nil
						}
					})
				if mode == "mixed" || mode == "invalid" {
					delivery.EXPECT().DeliverSelectionIssue(gomock.Any(), gomock.Any()).Return(nil)
				}
				var prepared *preparedSelection
				var err error
				if kind == HandlerKindModel {
					prepared, err = service.prepareModel(target.Provider, target.Model)
				} else {
					prepared, err = service.prepareReasoning(target.ReasoningChoice)
				}
				require.NoError(t, err)
				defer prepared.Release()

				// Act through the admitted selection with a real owning context.
				result := prepared.Run(ctx)

				// Assert no later handler, validation, or commit runs, and failure meaning preserves complete current causes.
				require.False(t, result.committed)
				require.ErrorIs(t, result.source(), context.Canceled)
				if mode == "cancel" {
					require.True(t, errtree.IsCurrentCancellation(result.source(), func(err error) bool {
						return errors.Is(err, context.Canceled)
					}))
				} else {
					var failure extension.NestedExtensionFailure
					require.ErrorAs(t, result.source(), &failure)
					require.True(t, failure.IsExtensionFailure())
					require.False(t, errtree.IsCurrentCancellation(result.source(), func(err error) bool {
						return errors.Is(err, context.Canceled)
					}))
					if mode != "invalid" {
						require.ErrorIs(t, result.source(), cause)
					}
				}
			})
		}
	}
}
