package run

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/samber/lo"
	"github.com/samber/mo"

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

var testModelDescriptor = model.Descriptor{
	Provider: "openai-codex", Model: "gpt-test",
	ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{},
	Pricing: mo.None[model.Pricing](),
}

// TestServiceRunStop preserves ordered history, streaming state, events, run ID, and settlement.
func TestServiceRunStop(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	descriptor := tool.Descriptor{Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`), ConstrainedSampling: mo.None[tool.ConstrainedSampling]()}
	tools.EXPECT().Tools().Return([]tool.Descriptor{descriptor})
	response := model.Response{
		Content: []model.Content{
			{Kind: model.ContentText, Text: mo.Some("hello"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
			{
				Kind: model.ContentReasoning, Text: mo.Some(""),
				ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "codex", API: "", Model: "", CompatibilityKey: mo.None[string]()}, Payload: []byte{1, 2, 3}}), Final: true, ToolCall: mo.None[model.ToolCall](),
			},
			{Kind: model.ContentText, Text: mo.Some(" world"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
		},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
	assert.True(t, result.ErrorMessage.IsNone())
	require.Len(t, service.History(), 2)
	assert.Equal(t, mo.Some(response), service.History()[1].Model)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	assert.Equal(t, mo.Some("run-1"), service.State().RunID)
	assert.True(t, service.State().PartialResponse.IsNone())
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
	expectedUpdate.Position = mo.Some(0)
	expectedUpdate.Content = mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some("hello"), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()})
	assert.Equal(t, expectedUpdate, update)
	_, err = service.Run(t.Context(), Request{RunID: "run-2", UserText: "blocked"})
	require.ErrorIs(t, err, ErrRunActive)
	require.NoError(t, service.Settle("run-1"))
	assert.Equal(t, StatusIdle, service.State().Status)
	assert.True(t, service.State().RunID.IsNone())
	assert.True(t, service.State().PartialResponse.IsNone())
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
		Content: []model.Content{testCallItem(calls[0]), testCallItem(calls[1])},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	stopResponse := model.Response{
		Content: []model.Content{testTextItem("done")},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
						Kind: StreamEventToolCallStart, Position: mo.Some(position), Preview: mo.Some(preview), Content: mo.None[model.Content](), Delta: mo.None[string](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
					}))
					preview.Fields = []model.ToolCallPreviewField{{
						Name: "value", Kind: model.ToolCallPreviewFieldPrefix,
						Prefix: mo.Some("1"), Value: mo.None[any](),
					}}
					require.NoError(t, update(StreamEvent{
						Kind: StreamEventToolCallDelta, Position: mo.Some(position), Preview: mo.Some(preview), Content: mo.None[model.Content](), Delta: mo.None[string](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
					}))
					require.NoError(t, update(StreamEvent{
						Kind: StreamEventToolCallEnd, Position: mo.Some(position), ToolCall: mo.Some(call), Content: mo.None[model.Content](), Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), Response: mo.None[model.Response](),
					}))
				}
				return emitStream(update, firstResponse, nil)
			}
			require.Len(t, request.History, 4)
			assert.Equal(t, "call-1", request.History[2].ToolResult.OrEmpty().CallID)
			assert.Equal(t, "call-2", request.History[3].ToolResult.OrEmpty().CallID)
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

	// Arrange old and new runtimes around a blocked first provider request.
	oldProvider := NewMockModelProvider(gomock.NewController(t))
	newProvider := NewMockModelProvider(gomock.NewController(t))
	runtime := NewMockModelRuntime(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	oldModel := model.Descriptor{Provider: "old-provider", Model: "old-model", ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing]()}
	newModel := model.Descriptor{Provider: "new-provider", Model: "new-model", ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing]()}
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
			assert.Equal(t, model.ReasoningChoiceLow, request.ReasoningChoice)
			close(requestStarted)
			<-releaseRequest
			return emitStream(update, model.Response{
				Content: []model.Content{
					{Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
					testCallItem(call),
				},
				Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
			require.Len(t, request.History[1].Model.OrEmpty().Content, 2)
			assert.Equal(t, model.ContentReasoning, request.History[1].Model.OrEmpty().Content[0].Kind)
			assert.Equal(t, "visible reasoning", request.History[1].Model.OrEmpty().Content[0].Text.OrEmpty())
			assert.Equal(t, agent.HistoryEntryToolResult, request.History[2].Kind)
			return emitStream(update, model.Response{
				Content: []model.Content{testTextItem("done")}, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
			}, nil)
		},
	)
	tools.EXPECT().Tools().Return(nil).Times(2)
	tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).Return(agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("result"), IsError: false,
	}, nil)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := New(
		testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, newMockHistoryStore(t),
	)
	result := make(chan error, 1)

	// Act by switching the runtime while the first provider request is active.
	go func() {
		_, err := service.Run(t.Context(), Request{RunID: "runtime-switch", UserText: "go"})
		result <- err
	}()

	<-requestStarted
	committed = true
	close(releaseRequest)

	// Assert the continuation uses the new runtime and preserves prior history.
	require.NoError(t, <-result)
	require.Len(t, service.History(), 4)
	assert.Equal(t, "done", service.History()[3].Model.OrEmpty().Content[0].Text.OrEmpty())
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
		Content: []model.Content{testCallItem(calls[0]), testCallItem(calls[1])},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	stop := model.Response{Content: []model.Content{testTextItem("done")}, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(toolUse, nil))
	toolErr := errors.New("tool operation failed")
	tools.EXPECT().Execute(gomock.Any(), calls[0], gomock.Any()).Return(
		agent.ToolResult{}, toolErr,
	)
	tools.EXPECT().Execute(gomock.Any(), calls[1], gomock.Any()).Return(
		agent.ToolResult{CallID: calls[1].ID, ToolName: calls[1].Name, Contents: tool.TextContents("ok"), IsError: false}, nil,
	)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			require.Len(t, request.History, 4)
			assert.Equal(t, "failed", request.History[2].ToolResult.OrEmpty().CallID)
			assert.True(t, request.History[2].ToolResult.OrEmpty().IsError)
			require.ErrorContains(t, errors.New(request.History[2].ToolResult.OrEmpty().Contents[0].Text.OrEmpty()), "tool operation failed")
			assert.Equal(t, "succeeded", request.History[3].ToolResult.OrEmpty().CallID)
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
		Content: []model.Content{testCallItem(call)},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
			return agent.ToolResult{},
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
	assert.True(t, history[2].ToolResult.OrEmpty().IsError)
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
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventContentStart, Position: mo.Some(0),
				Content: mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some(""), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}),
				Delta:   mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventTextDelta, Position: mo.Some(0),
				Content: mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some("partial"), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}),
				Delta:   mo.Some("partial"), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			return errors.New("provider transport failed")
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-failed-partial", UserText: "hi"})

	require.Error(t, err)
	history := service.History()
	require.Len(t, history, 2)
	require.Len(t, history[1].Model.OrEmpty().Content, 1)
	assert.Equal(t, "partial", history[1].Model.OrEmpty().Content[0].Text.OrEmpty())
	assert.True(t, history[1].Model.OrEmpty().Content[0].Final)
}

// TestServiceRunProviderFailureRejectsMalformedRetainedContent verifies invalid gaps do not enter history or terminal frames.
func TestServiceRunProviderFailureRejectsMalformedRetainedContent(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventContentStart, Position: mo.Some(1),
				Content: mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some(""), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}),
				Delta:   mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			return errors.New("provider transport failed")
		},
	)
	delivered := make([]Event, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-malformed-partial", UserText: "hi"})

	require.ErrorContains(t, err, "provider transport failed")
	require.ErrorContains(t, err, "unknown kind")
	require.Len(t, service.History(), 1)
	assert.True(t, service.State().PartialResponse.IsNone())
	assert.NotContains(t, eventTypes(delivered), EventMessageEnd)
	assert.NotContains(t, eventTypes(delivered), EventTurnEnd)
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
			{Kind: model.ContentToolCall, ToolCall: mo.Some(model.ToolCall{ID: "unsafe", Name: "read", Arguments: map[string]any{}}), Text: mo.None[string](), Final: false, ProviderContext: mo.None[model.ProviderContext]()},
		},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.Some(safeMessage),
		Provider: mo.Some(model.ProviderID("openai-codex")), Model: mo.Some(model.ID("gpt-test")),
		ResponseModel: mo.Some(actualModel), ResponseID: mo.Some("resp-failed"),
		Usage:       mo.Some(model.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5, CachedInputTokens: 0, CacheWriteTokens: 0, ReasoningTokens: 0}),
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
	assert.Equal(t, safeMessage, result.ErrorMessage.OrEmpty())
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, safeMessage, history[1].Model.OrEmpty().ErrorMessage.OrEmpty())
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
	assert.Equal(t, "resp-failed", history[1].Model.OrEmpty().ResponseID.OrEmpty())
	assert.Equal(t, model.ID("gpt-actual"), history[1].Model.OrEmpty().ResponseModel.OrEmpty())
	mutatedResponse := history[1].Model.OrEmpty()
	mutatedResponse.ResponseModel = mo.Some(model.ID("mutated"))
	history[1].Model = mo.Some(mutatedResponse)
	preservedHistory := service.History()
	assert.Equal(t, model.ID("gpt-actual"), preservedHistory[1].Model.OrEmpty().ResponseModel.OrEmpty())
	assert.Equal(t, int64(5), history[1].Model.OrEmpty().Usage.OrEmpty().TotalTokens)
	assert.Equal(t, []model.Diagnostic{{Code: "provider_error", Message: safeMessage}}, history[1].Model.OrEmpty().Diagnostics)
	assert.Len(t, history[1].Model.OrEmpty().Content, 2)
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
	assert.Equal(t, safeMessage, messageEnd.Message.OrEmpty().ErrorMessage.OrEmpty())
	assert.Equal(t, safeMessage, agentEnd.Agent.OrEmpty().ErrorMessage.OrEmpty())
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
			Content: []model.Content{testTextItem("partial")},
			Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
			require.Len(t, state.PartialResponse.OrEmpty().Content, 1)
			assert.Equal(t, "partial", state.PartialResponse.OrEmpty().Content[0].Text.OrEmpty())
			historyBefore := service.History()
			_, secondErr := service.Run(t.Context(), Request{RunID: "blocked", UserText: "no"})
			require.ErrorIs(t, secondErr, ErrRunActive)
			assert.Equal(t, historyBefore, service.History())
			close(release)
			synctest.Wait()
			require.Error(t, <-outcome)
		}
		assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
		assert.Empty(t, service.State().PartialResponse.OrEmpty().Content)
		history := service.History()
		require.Len(t, history, 2)
		assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
		assert.Equal(t, "Model request failed.", history[1].Model.OrEmpty().ErrorMessage.OrEmpty())
		assert.Len(t, service.ProjectHistory(), 1)
	})
}

// TestServiceRunRejectsUnknownTerminalOutcome verifies rejection before malformed history mutation.
func TestServiceRunRejectsUnknownTerminalOutcome(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(
		model.Response{
			Content: nil, Outcome: mo.Some(model.Outcome(99)), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		},
		nil,
	))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh,
		provider, hookrunner.New(nil, nil, nil), tools, events,
	)

	_, err := service.Run(t.Context(), Request{RunID: "run-unknown-outcome", UserText: "hi"})

	require.ErrorContains(t, err, "unsupported terminal model outcome 99")
	assert.True(t, service.State().PartialResponse.IsNone())
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
	for _, message := range history {
		assert.NotEqual(t, model.Outcome(99), message.Model.OrEmpty().Outcome.OrEmpty())
	}
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
			Content: []model.Content{testTextItem("partial")},
			Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
		assert.Equal(t, model.OutcomeAborted, history[1].Model.OrEmpty().Outcome.OrEmpty())
		assert.Equal(t, "Model request was canceled.", history[1].Model.OrEmpty().ErrorMessage.OrEmpty())
		assert.Empty(t, service.State().PartialResponse.OrEmpty().Content)
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
			Content: []model.Content{testCallItem(calls[0]), testCallItem(calls[1])},
			Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
		assert.Equal(t, "active", history[2].ToolResult.OrEmpty().CallID)
		assert.True(t, history[2].ToolResult.OrEmpty().IsError)
		projected := service.ProjectHistory()
		require.Len(t, projected, 4)
		assert.Equal(t, "skipped", projected[3].ToolResult.OrEmpty().CallID)
		assert.Equal(t, skippedCallError, projected[3].ToolResult.OrEmpty().Contents[0].Text.OrEmpty())
		assert.Len(t, service.History(), 3)
	})
}

// TestNormalizeTerminalResponseTreatsEmptyMessageAsAbsent preserves the prior empty-value fallback.
func TestNormalizeTerminalResponseTreatsEmptyMessageAsAbsent(t *testing.T) {
	t.Parallel()

	response := normalizeTerminalResponse(model.Response{
		Outcome: mo.Some(model.OutcomeFailed), ErrorMessage: mo.Some(""), Content: nil, Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	})

	assert.Equal(t, failedModelMessage, response.ErrorMessage.OrEmpty())
	assert.True(t, response.ErrorMessage.IsSome())
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
				model.Response{Content: nil, Outcome: mo.Some(testCase.modelOutcome), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}, nil,
			))
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

			result, err := service.Run(t.Context(), Request{RunID: "run-" + name, UserText: "hi"})

			require.Error(t, err)
			assert.Equal(t, testCase.runOutcome, result.Outcome)
			assert.Equal(t, testCase.errorMessage, result.ErrorMessage.OrEmpty())
			history := service.History()
			require.Len(t, history, 2)
			assert.Equal(t, testCase.errorMessage, history[1].Model.OrEmpty().ErrorMessage.OrEmpty())
			assert.Len(t, service.ProjectHistory(), 1)
		})
	}
}

func TestServiceRunStopsBeforeProviderWhenUserPersistenceFails(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := errors.New("persist user")
	store.EXPECT().Snapshot().Return(nil).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).Return(persistErr)
	observed := make([]EventType, 0, 2)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		observed = append(observed, event.Type)
		return nil
	}).Times(2)
	service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, store)

	_, err := service.Run(t.Context(), Request{RunID: "persist-user", UserText: "hello"})

	require.ErrorIs(t, err, persistErr)
	assert.Equal(t, []EventType{EventAgentStart, EventAgentEnd}, observed)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

func TestServiceRunStopsAfterCompletedToolWhenResultPersistenceFails(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := errors.New("persist tool result")
	call := model.ToolCall{ID: "call", Name: "write", Arguments: map[string]any{"path": "output.txt"}}
	response := model.Response{
		Content: []model.Content{testCallItem(call)}, Outcome: mo.Some(model.OutcomeToolUse),
		ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.Some("response-id"), Usage: mo.None[model.Usage](),
		Diagnostics: nil,
	}
	history := make([]agent.HistoryEntry, 0, 2)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(history) }).AnyTimes()
	gomock.InOrder(
		store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				history = append(history, cloneHistoryEntry(entry))
				return nil
			},
		),
		store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				require.Equal(t, agent.HistoryEntryModel, entry.Kind)
				history = append(history, cloneHistoryEntry(entry))
				return nil
			},
		),
		store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				require.Equal(t, agent.HistoryEntryToolResult, entry.Kind)
				return persistErr
			},
		),
	)
	runtime.EXPECT().Current().Return(RuntimeSelection{
		Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
	})
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			return emitStream(handle, response, nil)
		},
	)
	toolCompleted := false
	tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
		func(context.Context, model.ToolCall, tool.ProgressHandler) (agent.ToolResult, error) {
			toolCompleted = true
			return agent.ToolResult{
				CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("external effect complete"), IsError: false,
			}, nil
		},
	)
	observed := make([]EventType, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		observed = append(observed, event.Type)
		return nil
	}).AnyTimes()
	service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, store)

	_, err := service.Run(t.Context(), Request{RunID: "persist-tool", UserText: "write"})

	require.ErrorIs(t, err, persistErr)
	require.True(t, toolCompleted)
	assert.NotContains(t, observed, EventToolExecutionEnd)
	assert.NotContains(t, observed, EventToolResult)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

func TestServiceRunHidesMessageEndWhenModelPersistenceFails(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := errors.New("persist model")
	history := make([]agent.HistoryEntry, 0, 1)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(history) }).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, cloneHistoryEntry(entry))
			return nil
		},
	)
	store.EXPECT().Append(gomock.Any(), gomock.Any()).Return(persistErr)
	runtime.EXPECT().Current().Return(RuntimeSelection{
		Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
	})
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			return handle(StreamEvent{
				Kind: StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
				Response: mo.Some(model.Response{
					Content: []model.Content{testTextItem("done")}, Outcome: mo.Some(model.OutcomeStop),
					ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](),
					Diagnostics: nil,
				}),
			})
		},
	)
	observed := make([]EventType, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		observed = append(observed, event.Type)
		return nil
	}).AnyTimes()
	service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, store)

	_, err := service.Run(t.Context(), Request{RunID: "persist-model", UserText: "hello"})

	require.ErrorIs(t, err, persistErr)
	assert.NotContains(t, observed, EventMessageEnd)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
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
	require.Empty(t, service.History())
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
		Content: []model.Content{testCallItem(call)},
		Outcome: mo.Some(model.OutcomeLength), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	stop := model.Response{Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(length, nil))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			require.Len(t, request.History, 3)
			assert.True(t, request.History[2].ToolResult.OrEmpty().IsError)
			return emitStream(update, stop, nil)
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookrunner.New(nil, nil, nil), tools, events)

	_, err := service.Run(t.Context(), Request{RunID: "run-length", UserText: "go"})

	require.NoError(t, err)
	require.Len(t, service.History(), 4)
	assert.Equal(t, "length-call", service.History()[2].ToolResult.OrEmpty().CallID)
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
	return New(instructions, runtime, hookRunner, tools, events, newMockHistoryStore(t))
}

func newMockHistoryStore(t *testing.T) *MockHistoryStore {
	t.Helper()
	store := NewMockHistoryStore(gomock.NewController(t))
	history := make([]agent.HistoryEntry, 0)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
		return cloneHistory(history)
	}).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, cloneHistoryEntry(entry))
			return nil
		},
	).AnyTimes()
	return store
}

// testTextItem creates one complete text content item.
func testTextItem(text string) model.Content {
	return model.Content{Kind: model.ContentText, Text: mo.Some(text), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}
}

// testCallItem creates one complete tool-call content item.
func testCallItem(call model.ToolCall) model.Content {
	return model.Content{Kind: model.ContentToolCall, ToolCall: mo.Some(call), Text: mo.None[string](), Final: false, ProviderContext: mo.None[model.ProviderContext]()}
}

// eventTypes extracts observable event order for compact assertions.
func eventTypes(events []Event) []EventType {
	return lo.Map(events, func(event Event, _ int) EventType {
		return event.Type
	})
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
		Kind: StreamEventContentStart, Position: mo.Some(position),
		Content: mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some(""), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}), Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
	}); err != nil {
		return err
	}
	if err := handle(StreamEvent{
		Kind: StreamEventTextDelta, Position: mo.Some(position),
		Content: mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some(text), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}), Delta: mo.Some(text), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
	}); err != nil {
		return err
	}
	return handle(StreamEvent{
		Kind: StreamEventContentEnd, Position: mo.Some(position),
		Content: mo.Some(model.Content{Kind: model.ContentText, Text: mo.Some(""), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}), Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
	})
}

// emitStream emits terminal content ends followed by one terminal event.
func emitStream(handle StreamHandler, response model.Response, streamErr error) error {
	kind := StreamEventDone
	if streamErr != nil {
		kind = StreamEventError
	}
	if err := handle(StreamEvent{Kind: kind, Response: mo.Some(response), Position: mo.None[int](), Content: mo.None[model.Content](), Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall]()}); err != nil {
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
			value.History[0].User.OrEmpty().Content[0].Text = mo.Some("first transformation")
			return value, nil
		},
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			contextSeen = value.History[0].User.OrEmpty().Content[0].Text.OrEmpty()
			value.History[0].User.OrEmpty().Content[0].Text = mo.Some("final transformation")
			return value, nil
		},
	}, nil, nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			assert.Equal(t, "final transformation", request.History[0].User.OrEmpty().Content[0].Text.OrEmpty())
			return handle(StreamEvent{Kind: StreamEventDone, Response: mo.Some(model.Response{Outcome: mo.Some(model.OutcomeStop), Content: nil, ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}), Position: mo.None[int](), Content: mo.None[model.Content](), Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall]()})
		},
	)
	service := newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, hookRunner, tools, events)

	result, err := service.Run(t.Context(), Request{RunID: "context-success", UserText: "persisted input"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	assert.Equal(t, "first transformation", contextSeen)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, "persisted input", history[0].User.OrEmpty().Content[0].Text.OrEmpty())
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
			value.History[0].User.OrEmpty().Content[0].Text = mo.Some("secret transformed context")
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
	assert.Equal(t, failedModelMessage, result.ErrorMessage.OrEmpty())
	assert.Zero(t, laterCalls)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, "persisted input", history[0].User.OrEmpty().Content[0].Text.OrEmpty())
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
	assert.Equal(t, failedModelMessage, history[1].Model.OrEmpty().ErrorMessage.OrEmpty())
	assert.Equal(t, []model.Diagnostic{{Code: "internal_hook_failed", Message: "context"}}, history[1].Model.OrEmpty().Diagnostics)
	assert.Equal(t, []agent.HistoryEntry{history[0]}, service.ProjectHistory())
}

// TestCloneToolResultClonesImageBytesInsideOption verifies history snapshots do not share mutable image data.
func TestCloneToolResultClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	original := agent.ToolResult{Contents: []tool.ResultContent{{
		Kind:  tool.ResultContentImage,
		Text:  mo.None[string](),
		Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2, 3}}),
	}}, CallID: "", ToolName: "", IsError: false,
	}
	cloned := cloneToolResult(original)
	image, ok := cloned.Contents[0].Image.Get()
	require.True(t, ok)
	image.Data[0] = 9

	assert.Equal(t, byte(1), original.Contents[0].Image.OrEmpty().Data[0])
}
