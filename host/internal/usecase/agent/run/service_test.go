package run

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

const testInstructions = "resolved coding instructions"

// TestServiceRunStop preserves ordered history, streaming state, events, run ID, and settlement.
func TestServiceRunStop(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	descriptor := tool.Descriptor{Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`)}
	tools.EXPECT().Tools().Return([]tool.Descriptor{descriptor})
	response := agent.ModelResponse{
		Items: []agent.ModelItem{
			{
				Kind: agent.ModelItemText, Text: "hello",
				ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
			},
			{
				Kind: agent.ModelItemProviderContext, Text: "",
				ProviderContext: agent.ProviderContext{ProviderID: "codex", Payload: []byte{1, 2, 3}},
				ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
			},
			{
				Kind: agent.ModelItemText, Text: " world",
				ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
			},
		},
		Outcome:      agent.ModelOutcomeStop,
		ErrorMessage: "",
	}
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update ModelUpdateHandler) (agent.ModelResponse, error) {
			assert.Equal(t, testInstructions, request.Instructions)
			require.Len(t, request.History, 1)
			assert.Equal(t, agent.HistoryEntryUser, request.History[0].Kind)
			assert.Equal(t, []tool.Descriptor{descriptor}, request.Tools)
			require.NoError(t, update(ModelUpdate{Position: 0, Delta: "hello"}))
			require.NoError(t, update(ModelUpdate{Position: 2, Delta: " world"}))
			return response, nil
		},
	)
	delivered := make([]Event, 0, 8)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		delivered = append(delivered, event)
		return nil
	}).Times(8)
	service := New(testInstructions, provider, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-1", UserText: "hi"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, service.History(), 2)
	assert.Equal(t, response, service.History()[1].Model)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	for _, event := range delivered {
		assert.Equal(t, "run-1", event.RunID)
	}
	assert.Equal(t, []EventType{EventAgentStart, EventTurnStart, EventMessageStart, EventMessageUpdate, EventMessageUpdate, EventMessageEnd, EventTurnEnd, EventAgentEnd}, eventTypes(delivered))
	update := delivered[3]
	expectedUpdate := newEvent(EventMessageUpdate, "run-1")
	expectedUpdate.Position = 0
	expectedUpdate.Delta = "hello"
	assert.Equal(t, expectedUpdate, update)
	_, err = service.Run(t.Context(), Request{RunID: "run-2", UserText: "blocked"})
	require.ErrorIs(t, err, ErrRunActive)
	require.NoError(t, service.Settle("run-1"))
	assert.Equal(t, StatusIdle, service.State().Status)
}

// TestServiceRunToolUse executes calls sequentially and stores results before the next provider request.
func TestServiceRunToolUse(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	calls := []agent.ToolCall{
		{ID: "call-1", Name: "first", Arguments: map[string]any{"value": float64(1)}},
		{ID: "call-2", Name: "second", Arguments: map[string]any{"nested": map[string]any{"ok": true}}},
	}
	firstResponse := agent.ModelResponse{
		Items:        []agent.ModelItem{testCallItem(calls[0]), testCallItem(calls[1])},
		Outcome:      agent.ModelOutcomeToolUse,
		ErrorMessage: "",
	}
	stopResponse := agent.ModelResponse{
		Items:        []agent.ModelItem{testTextItem("done")},
		Outcome:      agent.ModelOutcomeStop,
		ErrorMessage: "",
	}
	order := make([]string, 0, 4)
	providerCall := 0
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, _ ModelUpdateHandler) (agent.ModelResponse, error) {
			assert.Equal(t, testInstructions, request.Instructions)
			providerCall++
			order = append(order, "provider")
			if providerCall == 1 {
				return firstResponse, nil
			}
			require.Len(t, request.History, 4)
			assert.Equal(t, "call-1", request.History[2].ToolResult.CallID)
			assert.Equal(t, "call-2", request.History[3].ToolResult.CallID)
			return stopResponse, nil
		},
	).Times(2)
	for _, call := range calls {
		tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
			func(
				_ context.Context,
				current agent.ToolCall,
				handleProgress tool.ProgressHandler,
			) (agent.ToolResult, error) {
				order = append(order, current.ID)
				require.NoError(t, handleProgress(tool.Progress{
					Channel: tool.ProgressChannelStatus,
					Content: "running " + current.ID,
				}))
				return agent.ToolResult{
					CallID: current.ID, ToolName: current.Name, Content: "ok", IsError: false,
				}, nil
			},
		)
	}
	delivered := make([]Event, 0, 18)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	service := New(testInstructions, provider, tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-tools", UserText: "go"})

	require.NoError(t, err)
	assert.Equal(t, []string{"provider", "call-1", "call-2", "provider"}, order)
	assert.Equal(t, []EventType{
		EventAgentStart,
		EventTurnStart, EventMessageStart, EventMessageEnd,
		EventToolExecutionStart, EventToolExecutionUpdate, EventToolExecutionEnd, EventToolResult,
		EventToolExecutionStart, EventToolExecutionUpdate, EventToolExecutionEnd, EventToolResult,
		EventTurnEnd,
		EventTurnStart, EventMessageStart, EventMessageEnd, EventTurnEnd,
		EventAgentEnd,
	}, eventTypes(delivered))
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

// TestServiceRunToolErrorContinues stores the error result, finishes later calls, and requests the model again.
func TestServiceRunToolErrorContinues(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	calls := []agent.ToolCall{
		{ID: "failed", Name: "first", Arguments: map[string]any{}},
		{ID: "succeeded", Name: "second", Arguments: map[string]any{}},
	}
	toolUse := agent.ModelResponse{
		Items:        []agent.ModelItem{testCallItem(calls[0]), testCallItem(calls[1])},
		Outcome:      agent.ModelOutcomeToolUse,
		ErrorMessage: "",
	}
	stop := agent.ModelResponse{Items: []agent.ModelItem{testTextItem("done")}, Outcome: agent.ModelOutcomeStop, ErrorMessage: ""}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(toolUse, nil)
	toolErr := errors.New("tool operation failed")
	tools.EXPECT().Execute(gomock.Any(), calls[0], gomock.Any()).Return(
		agent.ToolResult{CallID: "", ToolName: "", Content: "", IsError: false}, toolErr,
	)
	tools.EXPECT().Execute(gomock.Any(), calls[1], gomock.Any()).Return(
		agent.ToolResult{CallID: calls[1].ID, ToolName: calls[1].Name, Content: "ok", IsError: false}, nil,
	)
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, _ ModelUpdateHandler) (agent.ModelResponse, error) {
			require.Len(t, request.History, 4)
			assert.Equal(t, "failed", request.History[2].ToolResult.CallID)
			assert.True(t, request.History[2].ToolResult.IsError)
			require.ErrorContains(t, errors.New(request.History[2].ToolResult.Content), "tool operation failed")
			assert.Equal(t, "succeeded", request.History[3].ToolResult.CallID)
			return stop, nil
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := New(testInstructions, provider, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-tool-error", UserText: "go"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

// TestServiceRunToolProgressDeliveryFailure ends without another model request.
func TestServiceRunToolProgressDeliveryFailure(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	call := agent.ToolCall{ID: "delivery", Name: "bash", Arguments: map[string]any{}}
	response := agent.ModelResponse{
		Items:        []agent.ModelItem{testCallItem(call)},
		Outcome:      agent.ModelOutcomeToolUse,
		ErrorMessage: "",
	}
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(response, nil)
	deliveryErr := errors.New("tool progress delivery failed")
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			if event.Type == EventToolExecutionUpdate {
				return deliveryErr
			}
			return nil
		},
	).AnyTimes()
	tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ agent.ToolCall, handleProgress tool.ProgressHandler) (agent.ToolResult, error) {
			err := handleProgress(tool.Progress{Channel: tool.ProgressChannelStdout, Content: "partial"})
			require.ErrorIs(t, err, deliveryErr)
			return agent.ToolResult{CallID: "", ToolName: "", Content: "", IsError: false},
				fmt.Errorf("runtime propagated delivery: %w", err)
		},
	)
	service := New(testInstructions, provider, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-progress-error", UserText: "go"})

	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	history := service.History()
	require.Len(t, history, 3)
	assert.True(t, history[2].ToolResult.IsError)
}

// TestServiceRunProviderFailurePreservesSafeMessage keeps provider-approved detail in every terminal payload.
func TestServiceRunProviderFailurePreservesSafeMessage(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	safeMessage := "Provider rate limit reached."
	response := agent.ModelResponse{
		Items:        []agent.ModelItem{testTextItem("partial")},
		Outcome:      agent.ModelOutcomeStop,
		ErrorMessage: safeMessage,
	}
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		response, errors.New("provider transport failed"),
	)
	delivered := make([]Event, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	service := New(testInstructions, provider, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-safe-error", UserText: "go"})

	require.Error(t, err)
	assert.Equal(t, safeMessage, result.ErrorMessage)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, safeMessage, history[1].Model.ErrorMessage)
	var messageEnd Event
	var agentEnd Event
	for _, event := range delivered {
		if event.Type == EventMessageEnd {
			messageEnd = event
		}
		if event.Type == EventAgentEnd {
			agentEnd = event
		}
	}
	assert.Equal(t, safeMessage, messageEnd.Message.ErrorMessage)
	assert.Equal(t, safeMessage, agentEnd.Agent.ErrorMessage)
}

// TestServiceRunProviderFailure exposes partial state, stores failed safe content, and excludes it from projection.
func TestServiceRunProviderFailure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		provider := NewMockModelProvider(gomock.NewController(t))
		tools := NewMockToolRuntime(gomock.NewController(t))
		events := NewMockEventSink(gomock.NewController(t))
		streamed := make(chan struct{})
		release := make(chan struct{})
		tools.EXPECT().Tools().Return(nil)
		partial := agent.ModelResponse{
			Items:        []agent.ModelItem{testTextItem("partial")},
			Outcome:      agent.ModelOutcomeStop,
			ErrorMessage: "",
		}
		provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ModelRequest, update ModelUpdateHandler) (agent.ModelResponse, error) {
				require.NoError(t, update(ModelUpdate{Position: 0, Delta: "partial"}))
				close(streamed)
				<-release
				return partial, errors.New("provider secret")
			},
		)
		events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		service := New(testInstructions, provider, tools, events)
		outcome := make(chan error, 1)
		go func() {
			_, err := service.Run(t.Context(), Request{RunID: "run-failed", UserText: "hi"})
			outcome <- err
		}()
		select {
		case earlyErr := <-outcome:
			require.NoError(t, earlyErr)
		case <-streamed:
			state := service.State()
			assert.Equal(t, StatusRunning, state.Status)
			require.Len(t, state.PartialResponse.Items, 1)
			assert.Equal(t, "partial", state.PartialResponse.Items[0].Text)
			historyBefore := service.History()
			_, secondErr := service.Run(t.Context(), Request{RunID: "blocked", UserText: "no"})
			require.ErrorIs(t, secondErr, ErrRunActive)
			assert.Equal(t, historyBefore, service.History())
			close(release)
			synctest.Wait()
			require.Error(t, <-outcome)
		}
		assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
		assert.Empty(t, service.State().PartialResponse.Items)
		history := service.History()
		require.Len(t, history, 2)
		assert.Equal(t, agent.ModelOutcomeFailed, history[1].Model.Outcome)
		assert.Equal(t, "Model request failed.", history[1].Model.ErrorMessage)
		assert.Len(t, service.ProjectHistory(), 1)
	})
}

// TestServiceRunProviderCancellation uses a live terminal context and stores an aborted partial response.
func TestServiceRunProviderCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		provider := NewMockModelProvider(gomock.NewController(t))
		tools := NewMockToolRuntime(gomock.NewController(t))
		events := NewMockEventSink(gomock.NewController(t))
		ctx, cancel := context.WithCancel(t.Context())
		terminalContextErr := errors.New("terminal event received canceled context")
		tools.EXPECT().Tools().Return(nil)
		partial := agent.ModelResponse{
			Items:        []agent.ModelItem{testTextItem("partial")},
			Outcome:      agent.ModelOutcomeStop,
			ErrorMessage: "",
		}
		provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ModelRequest, update ModelUpdateHandler) (agent.ModelResponse, error) {
				require.NoError(t, update(ModelUpdate{Position: 0, Delta: "partial"}))
				cancel()
				return partial, context.Canceled
			},
		)
		events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
			func(deliveryContext context.Context, event Event) error {
				if event.Type >= EventMessageEnd && deliveryContext.Err() != nil {
					return terminalContextErr
				}
				return nil
			},
		).AnyTimes()
		service := New(testInstructions, provider, tools, events)

		_, err := service.Run(ctx, Request{RunID: "run-provider-cancel", UserText: "hi"})

		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, terminalContextErr)
		history := service.History()
		require.Len(t, history, 2)
		assert.Equal(t, agent.ModelOutcomeAborted, history[1].Model.Outcome)
		assert.Equal(t, "Model request was canceled.", history[1].Model.ErrorMessage)
		assert.Empty(t, service.State().PartialResponse.Items)
	})
}

// TestServiceRunCancellationPersistsOnlyActiveToolResult and synthesizes skipped results in projection.
func TestServiceRunCancellationPersistsOnlyActiveToolResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		provider := NewMockModelProvider(gomock.NewController(t))
		tools := NewMockToolRuntime(gomock.NewController(t))
		events := NewMockEventSink(gomock.NewController(t))
		calls := []agent.ToolCall{{ID: "active", Name: "bash", Arguments: map[string]any{}}, {ID: "skipped", Name: "edit", Arguments: map[string]any{}}}
		response := agent.ModelResponse{
			Items:        []agent.ModelItem{testCallItem(calls[0]), testCallItem(calls[1])},
			Outcome:      agent.ModelOutcomeToolUse,
			ErrorMessage: "",
		}
		tools.EXPECT().Tools().Return(nil)
		provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(response, nil)
		started := make(chan struct{})
		tools.EXPECT().Execute(gomock.Any(), calls[0], gomock.Any()).DoAndReturn(
			func(ctx context.Context, call agent.ToolCall, _ tool.ProgressHandler) (agent.ToolResult, error) {
				close(started)
				<-ctx.Done()
				return agent.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "", IsError: false}, ctx.Err()
			},
		)
		events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		service := New(testInstructions, provider, tools, events)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		outcome := make(chan error, 1)
		go func() {
			_, err := service.Run(ctx, Request{RunID: "run-cancel", UserText: "go"})
			outcome <- err
		}()
		select {
		case earlyErr := <-outcome:
			require.ErrorIs(t, earlyErr, context.Canceled)
		case <-started:
			cancel()
			synctest.Wait()
			require.ErrorIs(t, <-outcome, context.Canceled)
		}

		history := service.History()
		require.Len(t, history, 3)
		assert.Equal(t, "active", history[2].ToolResult.CallID)
		assert.True(t, history[2].ToolResult.IsError)
		projected := service.ProjectHistory()
		require.Len(t, projected, 4)
		assert.Equal(t, "skipped", projected[3].ToolResult.CallID)
		assert.Equal(t, skippedCallError, projected[3].ToolResult.Content)
		assert.Len(t, service.History(), 3)
	})
}

// TestServiceRunTerminalProviderOutcomes supplies safe errors and executes no calls.
func TestServiceRunTerminalProviderOutcomes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		modelOutcome agent.ModelOutcome
		runOutcome   agent.RunOutcome
		errorMessage string
	}{
		"aborted": {
			modelOutcome: agent.ModelOutcomeAborted,
			runOutcome:   agent.RunOutcomeAborted,
			errorMessage: abortedModelMessage,
		},
		"failed": {
			modelOutcome: agent.ModelOutcomeFailed,
			runOutcome:   agent.RunOutcomeFailed,
			errorMessage: failedModelMessage,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provider := NewMockModelProvider(gomock.NewController(t))
			tools := NewMockToolRuntime(gomock.NewController(t))
			events := NewMockEventSink(gomock.NewController(t))
			tools.EXPECT().Tools().Return(nil)
			provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(
				agent.ModelResponse{Items: nil, Outcome: testCase.modelOutcome, ErrorMessage: ""}, nil,
			)
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			service := New(testInstructions, provider, tools, events)

			result, err := service.Run(t.Context(), Request{RunID: "run-" + name, UserText: "hi"})

			require.Error(t, err)
			assert.Equal(t, testCase.runOutcome, result.Outcome)
			assert.Equal(t, testCase.errorMessage, result.ErrorMessage)
			history := service.History()
			require.Len(t, history, 2)
			assert.Equal(t, testCase.errorMessage, history[1].Model.ErrorMessage)
			assert.Len(t, service.ProjectHistory(), 1)
		})
	}
}

// TestServiceRunEventDeliveryFailure ends the run, attempts agent_end, and starts no provider or tool work.
func TestServiceRunEventDeliveryFailure(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	deliveryErr := errors.New("Host delivery failed")
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			assert.Equal(t, EventAgentStart, event.Type)
			return deliveryErr
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			assert.Equal(t, EventAgentEnd, event.Type)
			return nil
		},
	)
	service := New(testInstructions, provider, tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-delivery", UserText: "hi"})

	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	require.Len(t, service.History(), 1)
	_, err = service.Run(t.Context(), Request{RunID: "blocked", UserText: "no"})
	require.ErrorIs(t, err, ErrRunActive)
}

// TestServiceRunLengthWithCalls stores error results without executing tools and continues.
func TestServiceRunLengthWithCalls(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	call := agent.ToolCall{ID: "length-call", Name: "read", Arguments: map[string]any{"path": "x"}}
	length := agent.ModelResponse{
		Items:        []agent.ModelItem{testCallItem(call)},
		Outcome:      agent.ModelOutcomeLength,
		ErrorMessage: "",
	}
	stop := agent.ModelResponse{Items: nil, Outcome: agent.ModelOutcomeStop, ErrorMessage: ""}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).Return(length, nil)
	provider.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, _ ModelUpdateHandler) (agent.ModelResponse, error) {
			require.Len(t, request.History, 3)
			assert.True(t, request.History[2].ToolResult.IsError)
			return stop, nil
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := New(testInstructions, provider, tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-length", UserText: "go"})

	require.NoError(t, err)
	require.Len(t, service.History(), 4)
	assert.Equal(t, "length-call", service.History()[2].ToolResult.CallID)
}

// testTextItem creates one complete text content item.
func testTextItem(text string) agent.ModelItem {
	return agent.ModelItem{
		Kind:            agent.ModelItemText,
		Text:            text,
		ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
		ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
	}
}

// testCallItem creates one complete tool-call content item.
func testCallItem(call agent.ToolCall) agent.ModelItem {
	return agent.ModelItem{
		Kind:            agent.ModelItemToolCall,
		Text:            "",
		ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
		ToolCall:        call,
	}
}

// eventTypes extracts observable event order for compact assertions.
func eventTypes(events []Event) []EventType {
	result := make([]EventType, len(events))
	for index, event := range events {
		result[index] = event.Type
	}
	return result
}
