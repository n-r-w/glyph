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
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// returnedImage is the media type of the image that a returned message carries.
const returnedImage = "image/png"

// returnedHarness runs one agent run in which the client queues messages and records the terminal summary.
type returnedHarness struct {
	// service is the Agent Core service under test.
	service *Service
	// extensionDelivery rejects delivery calls unless a scenario explicitly expects them.
	extensionDelivery *MockExtensionMessageDelivery
	// summary is the run summary of the agent_end event.
	summary agent.RunSummary
	// requests holds the history of each provider request.
	requests [][]agent.HistoryEntry
	// imageData is the storage of the image that the client sent with its first steer message.
	imageData []byte
}

// queueClientMessages queues, in this arrival order, a steer, a followUp, and a steer message whose original form
// differs from the processed form.
func (h *returnedHarness) queueClientMessages(t *testing.T) {
	t.Helper()
	h.imageData = []byte{1, 2, 3}
	original := model.Message{Content: []model.InputContent{
		model.TextContent("original steer one"), model.ImageContent(returnedImage, h.imageData),
	}}
	for _, queued := range []struct {
		delivery agent.MessageDelivery
		message  runcontrol.QueuedMessage
	}{
		{agent.MessageDeliverySteer, runcontrol.QueuedMessage{
			Original: original, Processed: model.TextMessage("processed steer one"),
		}},
		{agent.MessageDeliveryFollowUp, runcontrol.QueuedMessage{
			Original: model.TextMessage("original follow-up"), Processed: model.TextMessage("processed follow-up"),
		}},
		{agent.MessageDeliverySteer, runcontrol.QueuedMessage{
			Original: model.TextMessage("original steer two"), Processed: model.TextMessage("processed steer two"),
		}},
	} {
		require.NoError(t, h.service.Enqueue("run", queued.delivery, queued.message))
	}
	for _, delivery := range []agent.MessageDelivery{agent.MessageDeliverySteer, agent.MessageDeliveryFollowUp} {
		require.NoError(t, h.service.EnqueueExtension("run", delivery, session.QueuedExtensionMessage{
			Session:           session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
			RuntimeInstanceID: "runtime", Message: session.ExtensionMessage{
				ExtensionID: "extension",
				EntryType:   "kind",
				Text:        "extension pending",
				Visibility:  session.ClientVisibilityHidden,
			},
		}))
	}
}

// returnedSetup selects the collaborators and the provider behavior of one returned-messages harness.
type returnedSetup struct {
	// store keeps the history of the run.
	store HistoryStore
	// tools executes the tool calls of the run.
	tools ToolRuntime
	// finalizer supplies the final values of model responses and tool results.
	finalizer MessageFinalizer
	// provider decides how each provider request ends; cancel cancels the run context.
	provider func(ctx context.Context, cancel context.CancelFunc, handle StreamHandler) error
	// deliveryErr fails the first turn-start event delivery when it is set.
	deliveryErr error
}

// newReturnedSetup returns a setup with an in-memory store, no tools, a pass-through finalizer, and the provider.
func newReturnedSetup(
	t *testing.T,
	provider func(context.Context, context.CancelFunc, StreamHandler) error,
) returnedSetup {
	t.Helper()
	return returnedSetup{
		store: newMockHistoryStore(t), tools: newNoTools(t), finalizer: passThroughFinalizer(t),
		provider: provider, deliveryErr: nil,
	}
}

// stopProvider ends every provider request with a final response.
func stopProvider(_ context.Context, _ context.CancelFunc, handle StreamHandler) error {
	return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
}

// returnedToolCall is the only tool call that toolUseProvider requests.
var returnedToolCall = model.ToolCall{ID: "call-1", Name: "first", Arguments: testToolCallArguments(`{}`)}

// toolUseProvider answers the first provider request with the tool call and later requests with a final response.
func toolUseProvider() func(context.Context, context.CancelFunc, StreamHandler) error {
	requests := 0
	return func(_ context.Context, _ context.CancelFunc, handle StreamHandler) error {
		requests++
		if requests > 1 {
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		}
		toolUse := emptyModelResponse(model.OutcomeToolUse)
		toolUse.Content = []model.Content{testCallItem(returnedToolCall)}
		return emitStream(handle, toolUse, nil)
	}
}

// newReturnedHarness builds a service whose first turn start queues the client messages.
func newReturnedHarness(t *testing.T, cancel context.CancelFunc, setup returnedSetup) *returnedHarness {
	t.Helper()
	harness := new(returnedHarness)
	queued := false
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventAgentEnd {
			harness.summary = event.Agent.MustGet()
		}
		if event.Type == agent.EventTurnStart && !queued {
			queued = true
			harness.queueClientMessages(t)
			return setup.deliveryErr
		}
		return nil
	}).AnyTimes()
	modelProvider := NewMockModelProvider(gomock.NewController(t))
	modelProvider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request ModelRequest, handle StreamHandler) error {
			harness.requests = append(harness.requests, request.History)
			return setup.provider(ctx, cancel, handle)
		},
	).AnyTimes()
	runtime := NewMockEffectiveContextPort(gomock.NewController(t))
	runtime.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
		passThroughContext(testModelDescriptor, model.ReasoningChoiceOff, modelProvider),
	).AnyTimes()
	harness.service = New(testInstructions, runtime, setup.tools, events, setup.store, setup.finalizer)
	harness.extensionDelivery = NewMockExtensionMessageDelivery(gomock.NewController(t))
	harness.service.BindExtensionMessages(harness.extensionDelivery)
	return harness
}

// requireReturnedMessages checks the returned messages of a summary against the original client messages of
// newReturnedHarness, from the given position on, in arrival order.
func requireReturnedMessages(t *testing.T, summary agent.RunSummary, first int) {
	t.Helper()
	all := []agent.ReturnedMessage{
		{Delivery: agent.MessageDeliverySteer, Message: model.Message{Content: []model.InputContent{
			model.TextContent("original steer one"), model.ImageContent(returnedImage, []byte{1, 2, 3}),
		}}},
		{Delivery: agent.MessageDeliveryFollowUp, Message: model.TextMessage("original follow-up")},
		{Delivery: agent.MessageDeliverySteer, Message: model.TextMessage("original steer two")},
	}
	require.Equal(t, all[first:], summary.ReturnedMessages)
}

// requireOnlyRequestInHistories checks that no provider request and no history entry holds a client message.
func requireOnlyRequestInHistories(t *testing.T, harness *returnedHarness) {
	t.Helper()
	for _, history := range append(harness.requests, harness.service.History()) {
		require.Equal(t, []string{"request"}, userTexts(history))
	}
}

// TestRunReturnsPendingClientMessagesOnFailureAndAbort checks that every failure and abort path returns the
// pending steer and followUp messages in arrival order, in their original form, and gives none to the model.
func TestRunReturnsPendingClientMessagesOnFailureAndAbort(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		provider    func(context.Context, context.CancelFunc, StreamHandler) error
		deliveryErr error
		outcome     agent.RunOutcome
	}{
		"provider failure": {
			provider: func(_ context.Context, _ context.CancelFunc, handle StreamHandler) error {
				return emitStream(handle, emptyModelResponse(model.OutcomeFailed), nil)
			},
			deliveryErr: nil,
			outcome:     agent.RunOutcomeFailed,
		},
		"provider abort": {
			provider: func(_ context.Context, _ context.CancelFunc, handle StreamHandler) error {
				return emitStream(handle, emptyModelResponse(model.OutcomeAborted), nil)
			},
			deliveryErr: nil,
			outcome:     agent.RunOutcomeAborted,
		},
		"context cancellation": {
			provider: func(ctx context.Context, cancel context.CancelFunc, handle StreamHandler) error {
				cancel()
				return emitStream(handle, emptyModelResponse(model.OutcomeAborted), ctx.Err())
			},
			deliveryErr: nil,
			outcome:     agent.RunOutcomeAborted,
		},
		"event delivery failure": {
			provider:    stopProvider,
			deliveryErr: errors.New("turn start observer failed"),
			outcome:     agent.RunOutcomeFailed,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a run that has a steer, a followUp, and a steer message pending when it ends.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			setup := newReturnedSetup(t, test.provider)
			setup.deliveryErr = test.deliveryErr
			harness := newReturnedHarness(t, cancel, setup)

			// Act by running the request.
			result, err := harness.service.Run(
				ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
			)
			// The client changes its image storage after sending; the returned form must not follow.
			clear(harness.imageData)

			// Assert the run ended with the outcome, returned the messages as sent, and gave none to the model.
			require.Error(t, err)
			require.Equal(t, test.outcome, result.Outcome)
			require.Equal(t, test.outcome, harness.summary.Outcome)
			requireReturnedMessages(t, harness.summary, 0)
			requireOnlyRequestInHistories(t, harness)
			require.NoError(t, harness.service.Settle("run"))
		})
	}
}

// TestRunReturnsMessagesAfterModelPersistenceFailure checks that a run that fails while it persists its model
// response returns its pending client messages.
func TestRunReturnsMessagesAfterModelPersistenceFailure(t *testing.T) {
	t.Parallel()
	// Arrange a store that accepts the request and rejects the model response.
	persistErr := errors.New("model response cannot be stored")
	setup := newReturnedSetup(t, stopProvider)
	setup.store = newRejectingHistoryStore(t, func(entry agent.HistoryEntry) error {
		if entry.Kind == agent.HistoryEntryModel {
			return persistErr
		}
		return nil
	})
	harness := newReturnedHarness(t, func() {}, setup)

	// Act by running the request.
	result, err := harness.service.Run(
		t.Context(),
		runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
	)

	// Assert the run failed and returned all three messages.
	require.ErrorIs(t, err, persistErr)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	requireReturnedMessages(t, harness.summary, 0)
}

// TestRunReturnsMessagesAfterToolFailure checks that a run that fails while it executes, finalizes, or persists a
// tool call returns its pending client messages and gives none of them to the model.
func TestRunReturnsMessagesAfterToolFailure(t *testing.T) {
	t.Parallel()
	toolErr := errors.New("tool operation failed")
	finalizeErr := errors.New("tool result finalizer failed")
	persistErr := errors.New("tool result cannot be stored")
	tests := map[string]struct {
		// configure installs the failure into the setup.
		configure func(t *testing.T, setup *returnedSetup)
		cause     error
	}{
		"fatal tool execution failure": {
			configure: func(t *testing.T, setup *returnedSetup) {
				t.Helper()
				cause := NewMockFatalOperationFailure(gomock.NewController(t))
				cause.EXPECT().Error().Return(toolErr.Error()).AnyTimes()
				tools := NewMockToolRuntime(gomock.NewController(t))
				tools.EXPECT().Tools().Return(nil).AnyTimes()
				tools.EXPECT().Execute(gomock.Any(), returnedToolCall, gomock.Any()).Return(agent.ToolResult{}, cause)
				setup.tools = tools
			},
			cause: nil,
		},
		"tool result finalizer error": {
			configure: func(t *testing.T, setup *returnedSetup) {
				t.Helper()
				setup.tools = newExecutingTools(t)
				finalizer := NewMockMessageFinalizer(gomock.NewController(t))
				finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, response model.Response) (model.Response, error) { return response, nil },
				).AnyTimes()
				finalizer.EXPECT().
					FinalizeToolResult(gomock.Any(), gomock.Any()).
					Return(agent.ToolResult{}, finalizeErr)
				setup.finalizer = finalizer
			},
			cause: finalizeErr,
		},
		"tool result persistence failure": {
			configure: func(t *testing.T, setup *returnedSetup) {
				t.Helper()
				setup.tools = newExecutingTools(t)
				setup.store = newRejectingHistoryStore(t, func(entry agent.HistoryEntry) error {
					if entry.Kind == agent.HistoryEntryToolResult {
						return persistErr
					}
					return nil
				})
			},
			cause: persistErr,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a run whose first response calls a tool, with the failure installed.
			setup := newReturnedSetup(t, toolUseProvider())
			test.configure(t, &setup)
			harness := newReturnedHarness(t, func() {}, setup)

			// Act by running the request.
			result, err := harness.service.Run(
				t.Context(),
				runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
			)

			// Assert the run failed, returned all three messages, and gave none to the model.
			require.Error(t, err)
			if test.cause != nil {
				require.ErrorIs(t, err, test.cause)
			}
			require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			requireReturnedMessages(t, harness.summary, 0)
			requireOnlyRequestInHistories(t, harness)
		})
	}
}

// newExecutingTools returns a tool runtime whose only call succeeds.
func newExecutingTools(t *testing.T) *MockToolRuntime {
	t.Helper()
	tools := NewMockToolRuntime(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil).AnyTimes()
	tools.EXPECT().Execute(gomock.Any(), returnedToolCall, gomock.Any()).Return(
		agent.ToolResult{
			Terminate: false,
			CallID:    returnedToolCall.ID, ToolName: returnedToolCall.Name,
			Contents: tool.TextContents("ok"), IsError: false,
		}, nil,
	)
	return tools
}

// TestRunAllModeAppendFailureReturnsUndeliveredMessages checks that in all mode a failed append returns the message
// that failed, the later popped messages, and the messages that stayed pending, while the messages appended before
// the failure stay delivered.
func TestRunAllModeAppendFailureReturnsUndeliveredMessages(t *testing.T) {
	t.Parallel()
	// Arrange all mode and a store that rejects the second popped steer message.
	appendErr := errors.New("steer message cannot be stored")
	setup := newReturnedSetup(t, stopProvider)
	setup.store = newRejectingHistoryStore(t, func(entry agent.HistoryEntry) error {
		if user, present := entry.User.Get(); present && user.Content[0].Text.OrEmpty() == "processed steer two" {
			return appendErr
		}
		return nil
	})
	harness := newReturnedHarness(t, func() {}, setup)
	harness.service.SetQueueMode(agent.QueueModeAll)

	// Act by running the request; its final response pops both steer messages.
	result, err := harness.service.Run(
		t.Context(),
		runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
	)

	// Assert the first steer message is delivered, and the failed steer message and the pending followUp message
	// come back in arrival order.
	require.ErrorIs(t, err, appendErr)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.Equal(t, []string{"request", "processed steer one"}, userTexts(harness.service.History()))
	requireReturnedMessages(t, harness.summary, 1)
}

// TestRunCompletionReturnsNoMessages checks that a completed run returns no messages, because it delivered all of
// them to the model.
func TestRunCompletionReturnsNoMessages(t *testing.T) {
	t.Parallel()
	// Arrange a run whose provider always gives a final response.
	harness := newReturnedHarness(t, func() {}, newReturnedSetup(t, stopProvider))
	harness.extensionDelivery.EXPECT().DeliverExtensionMessage(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)

	// Act by running the request with three pending client messages.
	result, err := harness.service.Run(
		t.Context(),
		runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
	)

	// Assert the run completed, the model received the processed messages, and none came back.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Empty(t, harness.summary.ReturnedMessages)
	require.Equal(
		t,
		[]string{"request", "processed steer one", "processed steer two", "processed follow-up"},
		userTexts(harness.service.History()),
	)
}
