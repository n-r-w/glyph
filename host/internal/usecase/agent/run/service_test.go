//nolint:exhaustruct // Tests set only fields relevant to each event or response.
package run

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

const testInstructions = "resolved coding instructions"

var testModelDescriptor = model.Descriptor{Provider: "openai-codex", Model: "gpt-test"}

// TestServiceRunStop preserves ordered history, streaming state, events, run ID, and settlement.
func TestServiceRunStop(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	descriptor := tool.Descriptor{Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`)}
	tools.EXPECT().Tools().Return([]tool.Descriptor{descriptor})
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: "hello",
				ProviderContext: model.ProviderContext{Source: model.ProviderContextSource{ProviderID: ""}, Payload: nil},
				ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
			},
			{
				Kind: model.ContentReasoning, Text: "",
				ProviderContext: model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "codex"}, Payload: []byte{1, 2, 3}},
				ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
			},
			{
				Kind: model.ContentText, Text: " world",
				ProviderContext: model.ProviderContext{Source: model.ProviderContextSource{ProviderID: ""}, Payload: nil},
				ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
			},
		},
		Outcome:      model.OutcomeStop,
		ErrorMessage: "",
	}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			assert.Equal(t, testInstructions, request.Instructions)
			require.Len(t, request.History, 1)
			assert.Equal(t, agent.HistoryEntryUser, request.History[0].Kind)
			assert.Equal(t, []tool.Descriptor{descriptor}, request.Tools)
			require.NoError(t, emitText(update, 0, "hello"))
			require.NoError(t, emitText(update, 2, " world"))
			return emitStream(update, response, nil)
		},
	)
	delivered := make([]Event, 0, 12)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		delivered = append(delivered, event)
		return nil
	}).Times(12)
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-1", UserText: "hi"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, service.History(), 2)
	assert.Equal(t, response, service.History()[1].Model)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	for _, event := range delivered {
		assert.Equal(t, "run-1", event.RunID)
	}
	assert.Equal(t, []EventType{
		EventAgentStart, EventTurnStart, EventMessageStart,
		EventContentStart, EventTextDelta, EventContentEnd,
		EventContentStart, EventTextDelta, EventContentEnd,
		EventMessageEnd, EventTurnEnd, EventAgentEnd,
	}, eventTypes(delivered))
	update := delivered[4]
	expectedUpdate := newEvent(EventTextDelta, "run-1")
	expectedUpdate.Position = 0
	expectedUpdate.Content = model.Content{Kind: model.ContentText, Text: "hello"}
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
	calls := []model.ToolCall{
		{ID: "call-1", Name: "first", Arguments: map[string]any{"value": float64(1)}},
		{ID: "call-2", Name: "second", Arguments: map[string]any{"nested": map[string]any{"ok": true}}},
	}
	firstResponse := model.Response{
		Content:      []model.Content{testCallItem(calls[0]), testCallItem(calls[1])},
		Outcome:      model.OutcomeToolUse,
		ErrorMessage: "",
	}
	stopResponse := model.Response{
		Content:      []model.Content{testTextItem("done")},
		Outcome:      model.OutcomeStop,
		ErrorMessage: "",
	}
	order := make([]string, 0, 4)
	providerCall := 0
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			assert.Equal(t, testInstructions, request.Instructions)
			providerCall++
			order = append(order, "provider")
			if providerCall == 1 {
				for position, call := range calls {
					preview := model.ToolCallPreview{
						CallID: call.ID, Name: call.Name, Position: position,
						Provisional: true, Fields: nil,
					}
					require.NoError(t, update(StreamEvent{
						Kind: StreamEventToolCallStart, Position: position, Preview: preview,
					}))
					preview.Fields = []model.ToolCallPreviewField{{
						Name: "value", Kind: model.ToolCallPreviewFieldPrefix, Prefix: "1",
					}}
					require.NoError(t, update(StreamEvent{
						Kind: StreamEventToolCallDelta, Position: position, Preview: preview,
					}))
					require.NoError(t, update(StreamEvent{
						Kind: StreamEventToolCallEnd, Position: position, ToolCall: call,
					}))
				}
				return emitStream(update, firstResponse, nil)
			}
			require.Len(t, request.History, 4)
			assert.Equal(t, "call-1", request.History[2].ToolResult.CallID)
			assert.Equal(t, "call-2", request.History[3].ToolResult.CallID)
			return emitStream(update, stopResponse, nil)
		},
	).Times(2)
	for _, call := range calls {
		tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
			func(
				_ context.Context,
				current model.ToolCall,
				handleProgress tool.ProgressHandler,
			) (agent.ToolResult, error) {
				order = append(order, current.ID)
				require.NoError(t, handleProgress(tool.Progress{
					Channel: tool.ProgressChannelStatus,
					Content: "running " + current.ID,
				}))
				return agent.ToolResult{
					CallID: current.ID, ToolName: current.Name, Contents: tool.TextContents("ok"), IsError: false,
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
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-tools", UserText: "go"})

	require.NoError(t, err)
	assert.Equal(t, []string{"provider", "call-1", "call-2", "provider"}, order)
	assert.Equal(t, []EventType{
		EventAgentStart,
		EventTurnStart, EventMessageStart,
		EventToolCallStart, EventToolCallDelta, EventToolCallEnd,
		EventToolCallStart, EventToolCallDelta, EventToolCallEnd,
		EventMessageEnd,
		EventToolExecutionStart, EventToolExecutionUpdate, EventToolExecutionEnd, EventToolResult,
		EventToolExecutionStart, EventToolExecutionUpdate, EventToolExecutionEnd, EventToolResult,
		EventTurnEnd,
		EventTurnStart, EventMessageStart, EventMessageEnd, EventTurnEnd,
		EventAgentEnd,
	}, eventTypes(delivered))
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

// TestServiceReadsRuntimeBeforeEachProviderRequest verifies request snapshots and uninterrupted history.
func TestServiceReadsRuntimeBeforeEachProviderRequest(t *testing.T) {
	t.Parallel()

	oldProvider := NewMockModelProvider(gomock.NewController(t))
	newProvider := NewMockModelProvider(gomock.NewController(t))
	runtime := NewMockModelRuntime(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	oldModel := model.Descriptor{Provider: "old-provider", Model: "old-model"}
	newModel := model.Descriptor{Provider: "new-provider", Model: "new-model"}
	committed := false
	runtime.EXPECT().Current().DoAndReturn(func() RuntimeSelection {
		if committed {
			return RuntimeSelection{
				Model: newModel, ReasoningChoice: model.ReasoningChoiceHigh, Provider: newProvider,
			}
		}
		return RuntimeSelection{
			Model: oldModel, ReasoningChoice: model.ReasoningChoiceLow, Provider: oldProvider,
		}
	}).AnyTimes()
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	call := model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "file"}}
	oldProvider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			assert.Equal(t, oldModel, request.Model)
			close(requestStarted)
			<-releaseRequest
			return emitStream(update, model.Response{
				Content: []model.Content{testCallItem(call)}, Outcome: model.OutcomeToolUse,
			}, nil)
		},
	)
	oldProvider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("stale provider used")).AnyTimes()
	newProvider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			assert.Equal(t, newModel, request.Model)
			assert.Equal(t, model.ReasoningChoiceHigh, request.ReasoningChoice)
			require.Len(t, request.History, 3)
			assert.Equal(t, agent.HistoryEntryUser, request.History[0].Kind)
			assert.Equal(t, agent.HistoryEntryModel, request.History[1].Kind)
			assert.Equal(t, agent.HistoryEntryToolResult, request.History[2].Kind)
			return emitStream(update, model.Response{
				Content: []model.Content{testTextItem("done")}, Outcome: model.OutcomeStop,
			}, nil)
		},
	)
	tools.EXPECT().Tools().Return(nil).Times(2)
	tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).Return(agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("result"), IsError: false,
	}, nil)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events)
	result := make(chan error, 1)
	go func() {
		_, err := service.Run(t.Context(), Request{RunID: "runtime-switch", UserText: "go"})
		result <- err
	}()

	<-requestStarted
	committed = true
	close(releaseRequest)

	require.NoError(t, <-result)
	require.Len(t, service.History(), 4)
	assert.Equal(t, "done", service.History()[3].Model.Content[0].Text)
}

// TestServiceRunToolErrorContinues stores the error result, finishes later calls, and requests the model again.
func TestServiceRunToolErrorContinues(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	calls := []model.ToolCall{
		{ID: "failed", Name: "first", Arguments: map[string]any{}},
		{ID: "succeeded", Name: "second", Arguments: map[string]any{}},
	}
	toolUse := model.Response{
		Content:      []model.Content{testCallItem(calls[0]), testCallItem(calls[1])},
		Outcome:      model.OutcomeToolUse,
		ErrorMessage: "",
	}
	stop := model.Response{Content: []model.Content{testTextItem("done")}, Outcome: model.OutcomeStop, ErrorMessage: ""}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(toolUse, nil))
	toolErr := errors.New("tool operation failed")
	tools.EXPECT().Execute(gomock.Any(), calls[0], gomock.Any()).Return(
		agent.ToolResult{CallID: "", ToolName: "", Contents: nil, IsError: false}, toolErr,
	)
	tools.EXPECT().Execute(gomock.Any(), calls[1], gomock.Any()).Return(
		agent.ToolResult{CallID: calls[1].ID, ToolName: calls[1].Name, Contents: tool.TextContents("ok"), IsError: false}, nil,
	)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			require.Len(t, request.History, 4)
			assert.Equal(t, "failed", request.History[2].ToolResult.CallID)
			assert.True(t, request.History[2].ToolResult.IsError)
			require.ErrorContains(t, errors.New(request.History[2].ToolResult.Contents[0].Text), "tool operation failed")
			assert.Equal(t, "succeeded", request.History[3].ToolResult.CallID)
			return emitStream(update, stop, nil)
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

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
	call := model.ToolCall{ID: "delivery", Name: "bash", Arguments: map[string]any{}}
	response := model.Response{
		Content:      []model.Content{testCallItem(call)},
		Outcome:      model.OutcomeToolUse,
		ErrorMessage: "",
	}
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
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
		func(_ context.Context, _ model.ToolCall, handleProgress tool.ProgressHandler) (agent.ToolResult, error) {
			err := handleProgress(tool.Progress{Channel: tool.ProgressChannelStdout, Content: "partial"})
			require.ErrorIs(t, err, deliveryErr)
			return agent.ToolResult{CallID: "", ToolName: "", Contents: nil, IsError: false},
				fmt.Errorf("runtime propagated delivery: %w", err)
		},
	)
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-progress-error", UserText: "go"})

	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	history := service.History()
	require.Len(t, history, 3)
	assert.True(t, history[2].ToolResult.IsError)
}

// TestServiceRunProviderFailurePreservesStreamedText keeps partial text when the terminal error has no content.
func TestServiceRunProviderFailurePreservesStreamedText(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			require.NoError(t, emitText(handle, 0, "partial"))
			return emitStream(handle, model.Response{
				Content: nil, Outcome: model.OutcomeFailed, ErrorMessage: "Provider failed.",
			}, errors.New("provider transport failed"))
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-failed-partial", UserText: "hi"})

	require.Error(t, err)
	history := service.History()
	require.Len(t, history, 2)
	require.Len(t, history[1].Model.Content, 1)
	assert.Equal(t, "partial", history[1].Model.Content[0].Text)
}

// TestServiceRunProviderFailurePreservesSafeMessage keeps provider-approved detail in every terminal payload.
func TestServiceRunProviderFailurePreservesSafeMessage(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	safeMessage := "Provider rate limit reached."
	actualModel := model.ID("gpt-actual")
	response := model.Response{
		Content: []model.Content{
			testTextItem("partial"),
			{Kind: model.ContentToolCall, ToolCall: model.ToolCall{ID: "unsafe", Name: "read", Arguments: map[string]any{}}},
		},
		Outcome: model.OutcomeStop, ErrorMessage: safeMessage,
		Provider: "openai-codex", Model: "gpt-test", ResponseModel: &actualModel, ResponseID: "resp-failed",
		Usage:       model.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
		Diagnostics: []model.Diagnostic{{Code: "provider_error", Message: safeMessage}},
	}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, errors.New("provider transport failed")))
	delivered := make([]Event, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "run-safe-error", UserText: "go"})

	require.Error(t, err)
	assert.Equal(t, safeMessage, result.ErrorMessage)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, safeMessage, history[1].Model.ErrorMessage)
	assert.Equal(t, model.OutcomeFailed, history[1].Model.Outcome)
	assert.Equal(t, "resp-failed", history[1].Model.ResponseID)
	require.NotNil(t, history[1].Model.ResponseModel)
	assert.Equal(t, model.ID("gpt-actual"), *history[1].Model.ResponseModel)
	*history[1].Model.ResponseModel = "mutated"
	preservedHistory := service.History()
	require.NotNil(t, preservedHistory[1].Model.ResponseModel)
	assert.Equal(t, model.ID("gpt-actual"), *preservedHistory[1].Model.ResponseModel)
	assert.Equal(t, int64(5), history[1].Model.Usage.TotalTokens)
	assert.Equal(t, []model.Diagnostic{{Code: "provider_error", Message: safeMessage}}, history[1].Model.Diagnostics)
	assert.Len(t, history[1].Model.Content, 2)
	assert.Len(t, service.ProjectHistory(), 1)
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
		partial := model.Response{
			Content:      []model.Content{testTextItem("partial")},
			Outcome:      model.OutcomeStop,
			ErrorMessage: "",
		}
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ModelRequest, update StreamHandler) error {
				require.NoError(t, emitText(update, 0, "partial"))
				close(streamed)
				<-release
				return emitStream(update, partial, errors.New("provider secret"))
			},
		)
		events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)
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
			require.Len(t, state.PartialResponse.Content, 1)
			assert.Equal(t, "partial", state.PartialResponse.Content[0].Text)
			historyBefore := service.History()
			_, secondErr := service.Run(t.Context(), Request{RunID: "blocked", UserText: "no"})
			require.ErrorIs(t, secondErr, ErrRunActive)
			assert.Equal(t, historyBefore, service.History())
			close(release)
			synctest.Wait()
			require.Error(t, <-outcome)
		}
		assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
		assert.Empty(t, service.State().PartialResponse.Content)
		history := service.History()
		require.Len(t, history, 2)
		assert.Equal(t, model.OutcomeFailed, history[1].Model.Outcome)
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
		partial := model.Response{
			Content:      []model.Content{testTextItem("partial")},
			Outcome:      model.OutcomeStop,
			ErrorMessage: "",
		}
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ModelRequest, update StreamHandler) error {
				require.NoError(t, emitText(update, 0, "partial"))
				cancel()
				return emitStream(update, partial, context.Canceled)
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
		service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

		_, err := service.Run(ctx, Request{RunID: "run-provider-cancel", UserText: "hi"})

		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, terminalContextErr)
		history := service.History()
		require.Len(t, history, 2)
		assert.Equal(t, model.OutcomeAborted, history[1].Model.Outcome)
		assert.Equal(t, "Model request was canceled.", history[1].Model.ErrorMessage)
		assert.Empty(t, service.State().PartialResponse.Content)
	})
}

// TestServiceRunCancellationPersistsOnlyActiveToolResult and synthesizes skipped results in projection.
func TestServiceRunCancellationPersistsOnlyActiveToolResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		provider := NewMockModelProvider(gomock.NewController(t))
		tools := NewMockToolRuntime(gomock.NewController(t))
		events := NewMockEventSink(gomock.NewController(t))
		calls := []model.ToolCall{{ID: "active", Name: "bash", Arguments: map[string]any{}}, {ID: "skipped", Name: "edit", Arguments: map[string]any{}}}
		response := model.Response{
			Content:      []model.Content{testCallItem(calls[0]), testCallItem(calls[1])},
			Outcome:      model.OutcomeToolUse,
			ErrorMessage: "",
		}
		tools.EXPECT().Tools().Return(nil)
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
		started := make(chan struct{})
		tools.EXPECT().Execute(gomock.Any(), calls[0], gomock.Any()).DoAndReturn(
			func(ctx context.Context, call model.ToolCall, _ tool.ProgressHandler) (agent.ToolResult, error) {
				close(started)
				<-ctx.Done()
				return agent.ToolResult{CallID: call.ID, ToolName: call.Name, Contents: nil, IsError: false}, ctx.Err()
			},
		)
		events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)
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
		assert.Equal(t, skippedCallError, projected[3].ToolResult.Contents[0].Text)
		assert.Len(t, service.History(), 3)
	})
}

// TestServiceRunTerminalProviderOutcomes supplies safe errors and executes no calls.
func TestServiceRunTerminalProviderOutcomes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		modelOutcome model.Outcome
		runOutcome   agent.RunOutcome
		errorMessage string
	}{
		"aborted": {
			modelOutcome: model.OutcomeAborted,
			runOutcome:   agent.RunOutcomeAborted,
			errorMessage: abortedModelMessage,
		},
		"failed": {
			modelOutcome: model.OutcomeFailed,
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
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(
				model.Response{Content: nil, Outcome: testCase.modelOutcome, ErrorMessage: ""}, nil,
			))
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

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
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

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
	call := model.ToolCall{ID: "length-call", Name: "read", Arguments: map[string]any{"path": "x"}}
	length := model.Response{
		Content:      []model.Content{testCallItem(call)},
		Outcome:      model.OutcomeLength,
		ErrorMessage: "",
	}
	stop := model.Response{Content: nil, Outcome: model.OutcomeStop, ErrorMessage: ""}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(length, nil))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			require.Len(t, request.History, 3)
			assert.True(t, request.History[2].ToolResult.IsError)
			return emitStream(update, stop, nil)
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-length", UserText: "go"})

	require.NoError(t, err)
	require.Len(t, service.History(), 4)
	assert.Equal(t, "length-call", service.History()[2].ToolResult.CallID)
}

func newTestService(
	t *testing.T,
	instructions string,
	descriptor model.Descriptor,
	level model.ReasoningChoice,
	provider ModelProvider,
	hookRunner hooks.ContextRunner,
	tools ToolRuntime,
	events EventSink,
) *Service {
	t.Helper()
	runtime := NewMockModelRuntime(gomock.NewController(t))
	runtime.EXPECT().Current().Return(RuntimeSelection{
		Model: descriptor, ReasoningChoice: level, Provider: provider,
	}).AnyTimes()
	return New(instructions, runtime, hookRunner, tools, events)
}

// testTextItem creates one complete text content item.
func testTextItem(text string) model.Content {
	return model.Content{
		Kind:            model.ContentText,
		Text:            text,
		ProviderContext: model.ProviderContext{Source: model.ProviderContextSource{ProviderID: ""}, Payload: nil},
		ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
	}
}

// testCallItem creates one complete tool-call content item.
func testCallItem(call model.ToolCall) model.Content {
	return model.Content{
		Kind:            model.ContentToolCall,
		Text:            "",
		ProviderContext: model.ProviderContext{Source: model.ProviderContextSource{ProviderID: ""}, Payload: nil},
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

// streamResult returns one deterministic semantic provider stream for tests.
func streamResult(response model.Response, streamErr error) func(context.Context, ModelRequest, StreamHandler) error {
	return func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
		return emitStream(handle, response, streamErr)
	}
}

// emitText emits one complete text block.
func emitText(handle StreamHandler, position int, text string) error {
	if err := handle(StreamEvent{
		Kind: StreamEventContentStart, Position: position, Content: model.Content{Kind: model.ContentText},
	}); err != nil {
		return err
	}
	if err := handle(StreamEvent{
		Kind: StreamEventTextDelta, Position: position,
		Content: model.Content{Kind: model.ContentText}, Delta: text,
	}); err != nil {
		return err
	}
	return handle(StreamEvent{
		Kind: StreamEventContentEnd, Position: position, Content: model.Content{Kind: model.ContentText},
	})
}

// emitStream emits terminal content ends followed by one terminal event.
func emitStream(handle StreamHandler, response model.Response, streamErr error) error {
	kind := StreamEventDone
	if streamErr != nil {
		kind = StreamEventError
	}
	if err := handle(StreamEvent{Kind: kind, Response: response}); err != nil {
		return err
	}
	return streamErr
}

// TestServiceRunTransformsRequestLocalContext verifies sequential context replacement without history mutation.
func TestServiceRunTransformsRequestLocalContext(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	contextSeen := ""
	hookRunner := hookrunner.New([]hooks.ContextHandler{
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			value.History[0].User.Content[0].Text = "first transformation"
			return value, nil
		},
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			contextSeen = value.History[0].User.Content[0].Text
			value.History[0].User.Content[0].Text = "final transformation"
			return value, nil
		},
	}, nil, nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			assert.Equal(t, "final transformation", request.History[0].User.Content[0].Text)
			return handle(StreamEvent{Kind: StreamEventDone, Response: model.Response{Outcome: model.OutcomeStop}})
		},
	)
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookRunner, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "context-success", UserText: "persisted input"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	assert.Equal(t, "first transformation", contextSeen)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, "persisted input", history[0].User.Content[0].Text)
}

// TestServiceRunStopsOnContextHookFailure verifies safe terminal failure before provider invocation.
func TestServiceRunStopsOnContextHookFailure(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	laterCalls := 0
	hookRunner := hookrunner.New([]hooks.ContextHandler{
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			value.History[0].User.Content[0].Text = "secret transformed context"
			return hooks.Context{}, errors.New("secret raw hook error")
		},
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			laterCalls++
			return value, nil
		},
	}, nil, nil)
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookRunner, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "context-failure", UserText: "persisted input"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	assert.Equal(t, failedModelMessage, result.ErrorMessage)
	assert.Zero(t, laterCalls)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, "persisted input", history[0].User.Content[0].Text)
	assert.Equal(t, model.OutcomeFailed, history[1].Model.Outcome)
	assert.Equal(t, failedModelMessage, history[1].Model.ErrorMessage)
	assert.Equal(t, []model.Diagnostic{{Code: "internal_hook_failed", Message: "context"}}, history[1].Model.Diagnostics)
	assert.Equal(t, []agent.HistoryEntry{history[0]}, service.ProjectHistory())
}
