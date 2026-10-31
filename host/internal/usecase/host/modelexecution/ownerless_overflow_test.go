//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceOwnerlessOverflowHandlers checks handler authority before repeat admission on both entry points.
func TestServiceOwnerlessOverflowHandlers(t *testing.T) {
	t.Parallel()
	const providerID, modelID = "provider", "model"
	const extensionID, runtimeID = "retry-extension", "retry-runtime"
	const firstID, secondID, laterID = "first", "second", "later"
	for _, configured := range []bool{false, true} {
		for _, outcome := range []string{
			"cancel", "failure", "invalid action", "invalid decision", "preserve", "repeat", "disabled",
		} {
			t.Run(outcome+map[bool]string{false: "/ordinary", true: "/configured"}[configured], func(t *testing.T) {
				t.Parallel()
				// Arrange a single overflow attempt and ordered handlers with an accepted intermediate decision.
				controller := gomock.NewController(t)
				catalog := NewMockCatalogResolver(controller)
				provider := NewMockProviderAttempt(controller)
				handlers := NewMockRetryHandlers(controller)
				output := NewMockRetryOutput(controller)
				selection := model.Selection{
					Provider:        providerID,
					Model:           modelID,
					ReasoningChoice: model.ReasoningChoiceOff,
				}
				descriptor := modelDescriptor(selection)
				binding := CatalogBinding{
					Model:           descriptor,
					ReasoningChoice: selection.ReasoningChoice,
					Provider:        provider,
				}
				if configured {
					catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(binding, nil)
				} else {
					catalog.EXPECT().ActiveBinding().Return(binding)
				}
				source := errors.New("complete oversized input source cause")
				handlerCause := errors.New("complete retry participant cause")
				provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
					Classification: ProviderFailureContextOverflow, RetryDelay: mo.None[time.Duration](), Cause: source,
				}).Times(1)
				output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Times(0)
				category := FailureContextLimit
				if outcome != "disabled" {
					first := RetryHandler{ExtensionID: extensionID, RuntimeID: runtimeID, HandlerID: firstID}
					second := RetryHandler{ExtensionID: extensionID, RuntimeID: runtimeID, HandlerID: secondID}
					later := RetryHandler{ExtensionID: extensionID, RuntimeID: runtimeID, HandlerID: laterID}
					handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{first, second, later})
					original := RetryDecision{Retryable: false, Retry: false, Delay: 0, AttemptLimit: 2}
					accepted := RetryDecision{Retryable: false, Retry: false, Delay: time.Millisecond, AttemptLimit: 3}
					action := RetryAction{Kind: RetryActionPreserve, Decision: mo.None[RetryDecision]()}
					var participantErr error
					switch outcome {
					case "cancel":
						action.Kind = RetryActionCancel
						category = FailureRetryCanceled
					case "failure":
						participantErr = handlerCause
						category = FailureExtensionFailed
					case "invalid action":
						action = RetryAction{Kind: RetryActionReplace, Decision: mo.None[RetryDecision]()}
						category = FailureExtensionFailed
					case "invalid decision":
						action = RetryAction{Kind: RetryActionReplace, Decision: mo.Some(RetryDecision{
							Retryable: true, Retry: true, Delay: -time.Second, AttemptLimit: 3,
						})}
						category = FailureExtensionFailed
					case "repeat":
						action = RetryAction{Kind: RetryActionReplace, Decision: mo.Some(RetryDecision{
							Retryable: true, Retry: true, Delay: time.Hour, AttemptLimit: 3,
						})}
					}
					firstCall := handlers.EXPECT().HandleRetry(gomock.Any(), first, gomock.Any()).DoAndReturn(
						func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
							assert.Equal(t, original, invocation.Original)
							assert.Equal(t, original, invocation.Current)
							assert.EqualValues(t, 1, invocation.CompletedAttempts)
							assert.Equal(t, ProviderFailureContextOverflow, invocation.Classification)
							assert.Contains(t, invocation.SourceError, source.Error())
							return RetryAction{Kind: RetryActionReplace, Decision: mo.Some(accepted)}, nil
						},
					)
					secondCall := handlers.EXPECT().
						HandleRetry(gomock.Any(), second, gomock.Any()).
						After(firstCall).
						DoAndReturn(
							func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
								assert.Equal(t, original, invocation.Original)
								assert.Equal(t, accepted, invocation.Current)
								return action, participantErr
							},
						)
					if outcome == "preserve" || outcome == "repeat" {
						handlers.EXPECT().HandleRetry(gomock.Any(), later, gomock.Any()).After(secondCall).DoAndReturn(
							func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
								assert.Equal(t, original, invocation.Original)
								expected := accepted
								if outcome == "repeat" {
									expected = action.Decision.MustGet()
								}
								assert.Equal(t, expected, invocation.Current)
								return RetryAction{Kind: RetryActionPreserve, Decision: mo.None[RetryDecision]()}, nil
							},
						)
					} else {
						handlers.EXPECT().HandleRetry(gomock.Any(), later, gomock.Any()).Times(0)
					}
				}
				service := New(catalog, RetryPolicy{
					Enabled:          outcome != "disabled",
					MaxRetries:       1,
					Delays:           []time.Duration{0},
					MaxProviderDelay: time.Hour,
				}, handlers, output, newTestConversationObservations(t))

				// Act through ordinary absent-owner or configured production execution.
				var err error
				if configured {
					bindPassThroughFinalizer(t, service)
					_, err = service.RequestConfigured(t.Context(), selection, "", nil,
						func(int64, int64, time.Duration, string) error {
							t.Error("unexpected retry progress")
							return nil
						})
				} else {
					err = streamLogical(t.Context(), service, agentrun.ContextRequest{
						Instructions: "",
						History:      nil,
						Tools:        nil,
					}, func(agentrun.StreamEvent) error { return nil })
				}

				// Assert complete causes, terminal category, stopped composition, and no repeat or progress.
				require.ErrorIs(t, err, source)
				var failure *LogicalFailureError
				require.ErrorAs(t, err, &failure)
				assert.Equal(t, category, failure.Category)
				switch outcome {
				case "failure":
					require.ErrorIs(t, err, handlerCause)
				case "invalid action":
					require.ErrorContains(t, err, "replace retry action requires a decision")
				case "invalid decision":
					assert.ErrorContains(t, err, "negative delay")
				}
			})
		}
	}
}
