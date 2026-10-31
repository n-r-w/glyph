//go:build !integration

package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// newStopProvider returns a provider that records each request history and always gives a final response.
func newStopProvider(t *testing.T, histories *[][]agent.HistoryEntry) *MockModelProvider {
	t.Helper()
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			*histories = append(*histories, request.History)
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).AnyTimes()
	return provider
}

// newNoTools returns a tool runtime without tools.
func newNoTools(t *testing.T) *MockToolRuntime {
	t.Helper()
	tools := NewMockToolRuntime(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil).AnyTimes()
	return tools
}

// queuedText returns a queued message whose original and processed forms are the same text.
func queuedText(text string) runcontrol.QueuedMessage {
	return runcontrol.QueuedMessage{Original: model.TextMessage(text), Processed: model.TextMessage(text)}
}

// userTexts returns the text of each user entry in history order.
func userTexts(history []agent.HistoryEntry) []string {
	texts := make([]string, 0, len(history))
	for _, entry := range history {
		if message, present := entry.User.Get(); present {
			texts = append(texts, message.Content[0].Text.OrEmpty())
		}
	}
	return texts
}

// TestEnqueueSteerAtRunCompletion checks that a steer message accepted at the end of the final turn starts another
// model request, and that Agent Core rejects a steer message after the run leaves the running state.
func TestEnqueueSteerAtRunCompletion(t *testing.T) {
	t.Parallel()
	// Arrange events that queue one message at the first turn end and another one at agent end.
	var histories [][]agent.HistoryEntry
	var service *Service
	var lateErr error
	turnEnds := 0
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		switch event.Type {
		case agent.EventTurnEnd:
			turnEnds++
			if turnEnds == 1 {
				require.NoError(t, service.Enqueue("run", agent.MessageDeliverySteer, queuedText("steer")))
			}
		case agent.EventAgentEnd:
			lateErr = service.Enqueue("run", agent.MessageDeliverySteer, queuedText("late"))
		default:
		}
		return nil
	}).AnyTimes()
	service = newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
		newStopProvider(t, &histories), newNoTools(t), events,
	)

	// Act by running one request whose first final response is followed by the steer message.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the second model request contains the steer message, and the late message was rejected.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 2)
	require.Equal(t, []string{"request"}, userTexts(histories[0]))
	require.Equal(t, []string{"request", "steer"}, userTexts(histories[1]))
	require.ErrorIs(t, lateErr, agent.ErrRunNotActive)
	require.NoError(t, service.Settle("run"))
	for _, delivery := range []agent.MessageDelivery{agent.MessageDeliverySteer, agent.MessageDeliveryFollowUp} {
		require.ErrorIs(t, service.Enqueue("run", delivery, queuedText("idle")), agent.ErrRunNotActive)
	}
}

// TestEnqueueNotCarriedToLaterRunAfterFailure checks that a steer or followUp message pending when a run fails reaches no later
// run.
func TestEnqueueNotCarriedToLaterRunAfterFailure(t *testing.T) {
	t.Parallel()
	// Arrange a provider whose first request queues a steer message and fails.
	var histories [][]agent.HistoryEntry
	var service *Service
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			histories = append(histories, request.History)
			if len(histories) == 1 {
				require.NoError(t, service.Enqueue("failed", agent.MessageDeliverySteer, queuedText("stale")))
				require.NoError(
					t,
					service.Enqueue("failed", agent.MessageDeliveryFollowUp, queuedText("stale-follow-up")),
				)
				return emitStream(handle, emptyModelResponse(model.OutcomeFailed), nil)
			}
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).Times(2)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff, provider,
		newNoTools(t), events)

	// Act by running the failing request, settling it, and running another request.
	failed, failedErr := service.Run(
		t.Context(),
		runcontrol.Request{RunID: "failed", Message: model.TextMessage("one")},
	)
	require.NoError(t, service.Settle("failed"))
	completed, err := service.Run(t.Context(), runcontrol.Request{RunID: "next", Message: model.TextMessage("two")})

	// Assert the failed run ended, and the next run made one model request without the stale message.
	require.Error(t, failedErr)
	require.Equal(t, agent.RunOutcomeFailed, failed.Outcome)
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, completed.Outcome)
	require.Len(t, histories, 2)
	require.NotContains(t, userTexts(service.History()), "stale")
	require.NotContains(t, userTexts(service.History()), "stale-follow-up")
}

// TestEnqueueRejectsEarlierRun checks that a message checked against one run is rejected while a later run is running.
func TestEnqueueRejectsEarlierRun(t *testing.T) {
	t.Parallel()
	// Arrange a provider that checks admission in the first run and queues for that run in the second run.
	var histories [][]agent.HistoryEntry
	var service *Service
	var checkedID, secondID string
	var staleErr error
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			histories = append(histories, request.History)
			var err error
			if len(histories) == 1 {
				checkedID, err = service.CheckEnqueue()
			} else {
				secondID, err = service.CheckEnqueue()
				staleErr = service.Enqueue(checkedID, agent.MessageDeliverySteer, queuedText("stale"))
			}
			require.NoError(t, err)
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).Times(2)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff, provider,
		newNoTools(t), events)

	// Act by running and settling the first request, then running the second request.
	_, err := service.Run(t.Context(), runcontrol.Request{RunID: "first", Message: model.TextMessage("one")})
	require.NoError(t, err)
	require.NoError(t, service.Settle("first"))
	_, err = service.Run(t.Context(), runcontrol.Request{RunID: "second", Message: model.TextMessage("two")})

	// Assert each check named its own run, and the second run rejected and never received the stale message.
	require.NoError(t, err)
	require.Equal(t, "first", checkedID)
	require.Equal(t, "second", secondID)
	require.ErrorIs(t, staleErr, agent.ErrRunNotActive)
	require.Len(t, histories, 2)
	require.NotContains(t, userTexts(service.History()), "stale")
}

// TestEnqueueFollowUpAtRunCompletion checks that steer takes precedence over followUp at the completion point, and
// that each delivery point gives the model one message.
func TestEnqueueFollowUpAtRunCompletion(t *testing.T) {
	t.Parallel()
	// Arrange events that queue two followUp messages and then two steer messages at the first turn end.
	var histories [][]agent.HistoryEntry
	var service *Service
	turnEnds := 0
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventTurnEnd {
			turnEnds++
			if turnEnds == 1 {
				for _, text := range []string{"follow-1", "follow-2"} {
					require.NoError(t, service.Enqueue("run", agent.MessageDeliveryFollowUp, queuedText(text)))
				}
				for _, text := range []string{"steer-1", "steer-2"} {
					require.NoError(t, service.Enqueue("run", agent.MessageDeliverySteer, queuedText(text)))
				}
			}
		}
		return nil
	}).AnyTimes()
	service = newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
		newStopProvider(t, &histories), newNoTools(t), events,
	)

	// Act by running one request whose first final response is followed by the queued messages.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert both steer messages precede both followUp messages, and each model request adds one message.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 5)
	require.Equal(t, []string{"request"}, userTexts(histories[0]))
	require.Equal(t, []string{"request", "steer-1"}, userTexts(histories[1]))
	require.Equal(t, []string{"request", "steer-1", "steer-2"}, userTexts(histories[2]))
	require.Equal(t, []string{"request", "steer-1", "steer-2", "follow-1"}, userTexts(histories[3]))
	require.Equal(t, []string{"request", "steer-1", "steer-2", "follow-1", "follow-2"}, userTexts(histories[4]))
}

// TestEnqueueFollowUpWaitsForToolBatch checks that a followUp message pending during a tool batch stays pending
// after the batch and reaches the model only after the final response.
func TestEnqueueFollowUpWaitsForToolBatch(t *testing.T) {
	t.Parallel()
	// Arrange a provider whose first response requests a tool and whose first request queues a followUp message.
	var histories [][]agent.HistoryEntry
	var service *Service
	call := model.ToolCall{ID: "call-1", Name: "first", Arguments: testToolCallArguments(`{}`)}
	toolUse := emptyModelResponse(model.OutcomeToolUse)
	toolUse.Content = []model.Content{testCallItem(call)}
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			histories = append(histories, request.History)
			if len(histories) == 1 {
				require.NoError(t, service.Enqueue("run", agent.MessageDeliveryFollowUp, queuedText("follow")))
				return emitStream(handle, toolUse, nil)
			}
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).Times(3)
	tools := NewMockToolRuntime(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil).AnyTimes()
	tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).Return(
		agent.ToolResult{CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("ok"), IsError: false}, nil,
	)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceOff,
		provider,
		tools,
		events,
	)

	// Act by running one request that uses a tool and then gives a final response.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the request after the tool batch lacks the followUp message, and the request after the final response has it.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 3)
	require.Equal(t, []string{"request"}, userTexts(histories[1]))
	require.Equal(t, []string{"request", "follow"}, userTexts(histories[2]))
}

// TestEnqueueRejectsUnknownDelivery checks that Agent Core refuses a delivery kind that it does not know.
func TestEnqueueRejectsUnknownDelivery(t *testing.T) {
	t.Parallel()
	// Arrange a running run that checks an unknown delivery kind.
	var enqueueErr error
	var service *Service
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			enqueueErr = service.Enqueue("run", agent.MessageDelivery(0), queuedText("unknown"))
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff, provider,
		newNoTools(t), events)

	// Act by running one request.
	_, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the enqueue failed with an error other than the inactive-run error.
	require.NoError(t, err)
	require.Error(t, enqueueErr)
	require.NotErrorIs(t, enqueueErr, agent.ErrRunNotActive)
}

// TestQueueModeAllDeliversEveryPendingMessage checks that mode all gives the model every pending steer message
// together, then every pending followUp message together, and that steer still takes precedence over followUp.
func TestQueueModeAllDeliversEveryPendingMessage(t *testing.T) {
	t.Parallel()
	// Arrange events that select mode all and queue two followUp and two steer messages at the first turn end.
	var histories [][]agent.HistoryEntry
	var service *Service
	turnEnds := 0
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventTurnEnd {
			turnEnds++
			if turnEnds == 1 {
				service.SetQueueMode(agent.QueueModeAll)
				for _, text := range []string{"follow-1", "follow-2"} {
					require.NoError(t, service.Enqueue("run", agent.MessageDeliveryFollowUp, queuedText(text)))
				}
				for _, text := range []string{"steer-1", "steer-2"} {
					require.NoError(t, service.Enqueue("run", agent.MessageDeliverySteer, queuedText(text)))
				}
			}
		}
		return nil
	}).AnyTimes()
	service = newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
		newStopProvider(t, &histories), newNoTools(t), events,
	)

	// Act by running one request whose first final response is followed by the queued messages.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the steer messages reach one model request, and the followUp messages reach the next one.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 3)
	require.Equal(t, []string{"request"}, userTexts(histories[0]))
	require.Equal(t, []string{"request", "steer-1", "steer-2"}, userTexts(histories[1]))
	require.Equal(t, []string{"request", "steer-1", "steer-2", "follow-1", "follow-2"}, userTexts(histories[2]))
}

// TestQueueModeAppliesFromNextDeliveryPoint checks that a mode selected during an agent run applies at the next
// delivery point.
func TestQueueModeAppliesFromNextDeliveryPoint(t *testing.T) {
	t.Parallel()
	// Arrange events that queue four steer messages at the first turn end and select mode all at the second.
	var histories [][]agent.HistoryEntry
	var service *Service
	turnEnds := 0
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type != agent.EventTurnEnd {
			return nil
		}
		turnEnds++
		switch turnEnds {
		case 1:
			for _, text := range []string{"steer-1", "steer-2", "steer-3", "steer-4"} {
				require.NoError(t, service.Enqueue("run", agent.MessageDeliverySteer, queuedText(text)))
			}
		case 2:
			service.SetQueueMode(agent.QueueModeAll)
		default:
		}
		return nil
	}).AnyTimes()
	service = newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
		newStopProvider(t, &histories), newNoTools(t), events,
	)

	// Act by running one request whose final responses are followed by the queued messages.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the first message came alone, and the remaining messages came together after the mode changed.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 3)
	require.Equal(t, []string{"request", "steer-1"}, userTexts(histories[1]))
	require.Equal(t, []string{"request", "steer-1", "steer-2", "steer-3", "steer-4"}, userTexts(histories[2]))
}

// TestQueueModeAllDeliversEverySteerAfterToolBatch checks that mode all gives the next model request every steer
// message that is pending after a tool batch.
func TestQueueModeAllDeliversEverySteerAfterToolBatch(t *testing.T) {
	t.Parallel()
	// Arrange mode all, a provider whose first request queues two steer messages and requests a tool.
	var histories [][]agent.HistoryEntry
	var service *Service
	call := model.ToolCall{ID: "call-1", Name: "first", Arguments: testToolCallArguments(`{}`)}
	toolUse := emptyModelResponse(model.OutcomeToolUse)
	toolUse.Content = []model.Content{testCallItem(call)}
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			histories = append(histories, request.History)
			if len(histories) == 1 {
				for _, text := range []string{"steer-1", "steer-2"} {
					require.NoError(t, service.Enqueue("run", agent.MessageDeliverySteer, queuedText(text)))
				}
				return emitStream(handle, toolUse, nil)
			}
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).Times(2)
	tools := NewMockToolRuntime(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil).AnyTimes()
	tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).Return(
		agent.ToolResult{CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("ok"), IsError: false}, nil,
	)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff, provider, tools, events,
	)
	service.SetQueueMode(agent.QueueModeAll)

	// Act by running one request that uses a tool and then gives a final response.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the request after the tool batch contains both steer messages in arrival order.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 2)
	require.Equal(t, []string{"request", "steer-1", "steer-2"}, userTexts(histories[1]))
}
