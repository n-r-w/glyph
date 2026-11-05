//go:build !integration

package run

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// nextTurnMessage returns a captured extension attachment with a distinct exact payload.
func nextTurnMessage(text string) session.QueuedExtensionMessage {
	return session.QueuedExtensionMessage{
		Session:           session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
		RuntimeInstanceID: "runtime",
		Message: session.ExtensionMessage{
			ExtensionID: "extension", EntryType: "attachment", Text: text, Visibility: session.ClientVisibilityHidden,
		},
	}
}

// persistNextTurn supplies the history projection of successful extension persistence to generated delivery mocks.
func persistNextTurn(service *Service) func(context.Context, session.QueuedExtensionMessage) (bool, error) {
	return func(ctx context.Context, message session.QueuedExtensionMessage) (bool, error) {
		err := service.appendUser(ctx, model.TextMessage(message.Message.Text))
		return err == nil, err
	}
}

// TestNextTurnStartBoundary checks idle acceptance, all-message delivery, and acceptance after run reservation.
func TestNextTurnStartBoundary(t *testing.T) {
	t.Parallel()
	for _, mode := range []agent.QueueMode{agent.QueueModeOneAtATime, agent.QueueModeAll} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			t.Parallel()
			// Arrange two idle attachments and one accepted by the first agent-start observer.
			var service *Service
			var histories [][]agent.HistoryEntry
			first, second, later := nextTurnMessage("first\nexact"), nextTurnMessage("second"), nextTurnMessage("later")
			events := NewMockEventSink(gomock.NewController(t))
			events.EXPECT().
				Deliver(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, event agent.Event) error {
					if event.Type == agent.EventAgentStart && event.RunID == "first-run" {
						require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, later))
					}
					return nil
				}).
				AnyTimes()
			service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
				newStopProvider(t, &histories), newNoTools(t), events)
			service.SetQueueMode(mode)
			delivery := NewMockExtensionMessageDelivery(gomock.NewController(t))
			gomock.InOrder(
				delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), first).DoAndReturn(persistNextTurn(service)),
				delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), second).DoAndReturn(persistNextTurn(service)),
				delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), later).DoAndReturn(persistNextTurn(service)),
			)
			service.BindExtensionMessages(delivery)

			// Act through two user runs, with no run started by queue acceptance.
			require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, first))
			require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, second))
			require.Equal(t, StatusIdle, service.State().Status)
			require.Empty(t, service.History())
			for _, id := range []string{"first-run", "second-run"} {
				result, err := service.Run(t.Context(), runcontrol.Request{RunID: id, Message: model.TextMessage(id)})
				require.NoError(t, err)
				require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
				require.NoError(t, service.Settle(id))
			}

			// Assert both idle attachments precede the first user entry and the late attachment waits for the second.
			require.Len(t, histories, 2)
			require.Equal(t, []string{"first\nexact", "second", "first-run"}, userTexts(histories[0]))
			require.Equal(
				t,
				[]string{"first\nexact", "second", "first-run", "later", "second-run"},
				userTexts(histories[1]),
			)
		})
	}
}

// TestNextTurnSurvivesFailureAndAbort checks preservation alongside client returns and extension discard.
func TestNextTurnSurvivesFailureAndAbort(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"failure", "abort", "agent-start"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			// Arrange pending next-user work before a run that cannot complete normally.
			var service *Service
			var summaries []agent.RunSummary
			var histories [][]agent.HistoryEntry
			attachment := nextTurnMessage("preserved")
			cause := errors.New("observer failed: original cause")
			events := NewMockEventSink(gomock.NewController(t))
			events.EXPECT().
				Deliver(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, event agent.Event) error {
					if event.Type == agent.EventAgentStart && event.RunID == "failed" {
						require.NoError(t, service.EnqueueExtension("", agent.MessageDeliveryNextTurn, attachment))
						require.NoError(
							t,
							service.EnqueueExtension(
								"failed",
								agent.MessageDeliveryFollowUp,
								nextTurnMessage("discarded"),
							),
						)
						require.NoError(
							t,
							service.Enqueue("failed", agent.MessageDeliverySteer, runcontrol.QueuedMessage{
								Original: model.TextMessage("original"), Processed: model.TextMessage("processed"),
							}),
						)
						if failure == "agent-start" {
							return cause
						}
					}
					if event.Type == agent.EventAgentEnd {
						summaries = append(summaries, event.Agent.MustGet())
					}
					return nil
				}).
				AnyTimes()
			provider := NewMockModelProvider(gomock.NewController(t))
			outcome := model.OutcomeFailed
			if failure == "abort" {
				outcome = model.OutcomeAborted
			}
			if failure != "agent-start" {
				provider.EXPECT().
					Stream(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, request ModelRequest, handle StreamHandler) error {
						histories = append(histories, request.History)
						return emitStream(handle, emptyModelResponse(outcome), nil)
					})
			}
			provider.EXPECT().
				Stream(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, request ModelRequest, handle StreamHandler) error {
					histories = append(histories, request.History)
					return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
				})
			service = newTestService(
				t,
				testInstructions,
				testModelDescriptor,
				model.ReasoningChoiceOff,
				provider,
				newNoTools(t),
				events,
			)
			delivery := NewMockExtensionMessageDelivery(gomock.NewController(t))
			delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), attachment).DoAndReturn(persistNextTurn(service))
			service.BindExtensionMessages(delivery)

			// Act through failure or abort, settle, and start the next user run.
			result, err := service.Run(
				t.Context(),
				runcontrol.Request{RunID: "failed", Message: model.TextMessage("first")},
			)
			require.Error(t, err)
			if failure == "abort" {
				require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
			} else {
				require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			}
			require.NoError(t, service.Settle("failed"))
			_, err = service.Run(t.Context(), runcontrol.Request{RunID: "next", Message: model.TextMessage("next")})

			// Assert only the client original returns, and only nextTurn survives for delivery.
			require.NoError(t, err)
			require.Equal(
				t,
				[]agent.ReturnedMessage{{Delivery: agent.MessageDeliverySteer, Message: model.TextMessage("original")}},
				summaries[0].ReturnedMessages,
			)
			require.Empty(t, summaries[1].ReturnedMessages)
			if failure == "agent-start" {
				require.Len(t, histories, 1)
				require.Equal(t, []string{"preserved", "next"}, userTexts(histories[0]))
			} else {
				require.Len(t, histories, 2)
				require.Equal(t, []string{"first"}, userTexts(histories[0]))
				require.Equal(t, []string{"first", "preserved", "next"}, userTexts(histories[1]))
			}
		})
	}
}
