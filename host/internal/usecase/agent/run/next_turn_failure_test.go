//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestNextTurnDeliveryFailureRespectsCommit checks retry of uncommitted work and no duplicate after
// publication failure.
func TestNextTurnDeliveryFailureRespectsCommit(t *testing.T) {
	t.Parallel()
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncommitted", true: "committed-publication"}[committed], func(t *testing.T) {
			t.Parallel()
			// Arrange a failed attachment, another start attachment, and a later arrival during delivery.
			var histories [][]agent.HistoryEntry
			events := NewMockEventSink(gomock.NewController(t))
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
				newStopProvider(t, &histories), newNoTools(t), events)
			first, second, later := nextTurnMessage("first"), nextTurnMessage("second"), nextTurnMessage("later")
			require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, first))
			require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, second))
			cause := errors.New("append or publication failed: complete original cause")
			delivery := NewMockExtensionMessageDelivery(gomock.NewController(t))
			failed := delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), first).DoAndReturn(
				func(ctx context.Context, message session.QueuedExtensionMessage) (bool, error) {
					require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, later))
					if committed {
						_, err := persistNextTurn(service)(ctx, message)
						require.NoError(t, err)
					}
					return committed, cause
				})
			previous := failed
			if !committed {
				previous = delivery.EXPECT().
					DeliverExtensionMessage(gomock.Any(), first).
					DoAndReturn(persistNextTurn(service)).
					After(previous)
			}
			previous = delivery.EXPECT().
				DeliverExtensionMessage(gomock.Any(), second).
				DoAndReturn(persistNextTurn(service)).
				After(previous)
			delivery.EXPECT().
				DeliverExtensionMessage(gomock.Any(), later).
				DoAndReturn(persistNextTurn(service)).
				After(previous)
			service.BindExtensionMessages(delivery)

			// Act through the delivery failure and two later user runs.
			result, err := service.Run(
				t.Context(),
				runcontrol.Request{RunID: "failed", Message: model.TextMessage("not appended")},
			)
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, cause.Error())
			require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			if committed {
				require.Equal(t, []string{"first"}, userTexts(service.History()))
			} else {
				require.Empty(t, service.History())
			}
			require.Empty(t, histories)
			require.NoError(t, service.Settle("failed"))
			for _, id := range []string{"retry", "no-duplicate"} {
				_, err = service.Run(t.Context(), runcontrol.Request{RunID: id, Message: model.TextMessage(id)})
				require.NoError(t, err)
				require.NoError(t, service.Settle(id))
			}

			// Assert both later model requests contain each attachment exactly once, before the retry user entry.
			require.Len(t, histories, 2)
			require.Equal(t, []string{"first", "second", "later", "retry"}, userTexts(histories[0]))
			require.Equal(t, []string{"first", "second", "later", "retry", "no-duplicate"}, userTexts(histories[1]))
		})
	}
}

// TestNextTurnStartFailurePreservesSnapshot checks reservation-time work and newer work after observer failure
// or cancellation.
func TestNextTurnStartFailurePreservesSnapshot(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "observer", true: "cancellation"}[canceled], func(t *testing.T) {
			t.Parallel()
			// Arrange one attachment before reservation and another in the start observer.
			var service *Service
			var histories [][]agent.HistoryEntry
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first, later := nextTurnMessage("first"), nextTurnMessage("later")
			cause := errors.New("start observer failed: complete cause")
			events := NewMockEventSink(gomock.NewController(t))
			events.EXPECT().
				Deliver(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, event agent.Event) error {
					if event.Type == agent.EventAgentStart && event.RunID == "failed" {
						require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, later))
						if canceled {
							cancel()
							return ctx.Err()
						}
						return cause
					}
					return nil
				}).
				AnyTimes()
			service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
				newStopProvider(t, &histories), newNoTools(t), events)
			require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, first))
			delivery := NewMockExtensionMessageDelivery(gomock.NewController(t))
			gomock.InOrder(
				delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), first).DoAndReturn(persistNextTurn(service)),
				delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), later).DoAndReturn(persistNextTurn(service)),
			)
			service.BindExtensionMessages(delivery)

			// Act through the failed start and then an uncanceled user request.
			result, err := service.Run(
				ctx,
				runcontrol.Request{RunID: "failed", Message: model.TextMessage("not appended")},
			)
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
			} else {
				require.ErrorIs(t, err, cause)
				require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			}
			require.Empty(t, service.History())
			require.NoError(t, service.Settle("failed"))
			_, err = service.Run(t.Context(), runcontrol.Request{RunID: "retry", Message: model.TextMessage("retry")})

			// Assert the next user run receives both attachments in their original order.
			require.NoError(t, err)
			require.Len(t, histories, 1)
			require.Equal(t, []string{"first", "later", "retry"}, userTexts(histories[0]))
		})
	}
}
