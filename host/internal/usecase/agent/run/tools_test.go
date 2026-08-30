package run

import (
	"context"
	"errors"
	"fmt"

	"testing"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

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
		Outcome: mo.Some(
			model.OutcomeToolUse,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	stopResponse := model.Response{
		Content: []model.Content{testTextItem("done")},
		Outcome: mo.Some(
			model.OutcomeStop,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
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
						Kind:     StreamEventToolCallStart,
						Position: mo.Some(position),
						Preview:  mo.Some(preview),
						Content:  mo.None[model.Content](),
						Delta:    mo.None[string](),
						ToolCall: mo.None[model.ToolCall](),
						Response: mo.None[model.Response](),
					}))
					preview.Fields = []model.ToolCallPreviewField{{
						Name: "value", Kind: model.ToolCallPreviewFieldPrefix,
						Prefix: mo.Some("1"), Value: mo.None[any](),
					}}
					require.NoError(t, update(StreamEvent{
						Kind:     StreamEventToolCallDelta,
						Position: mo.Some(position),
						Preview:  mo.Some(preview),
						Content:  mo.None[model.Content](),
						Delta:    mo.None[string](),
						ToolCall: mo.None[model.ToolCall](),
						Response: mo.None[model.Response](),
					}))
					require.NoError(t, update(StreamEvent{
						Kind:     StreamEventToolCallEnd,
						Position: mo.Some(position),
						ToolCall: mo.Some(call),
						Content:  mo.None[model.Content](),
						Delta:    mo.None[string](),
						Preview:  mo.None[model.ToolCallPreview](),
						Response: mo.None[model.Response](),
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
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		hookrunner.New(nil, nil, nil),
		tools,
		events,
	)

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
	oldModel := model.Descriptor{
		Provider:              "old-provider",
		Model:                 "old-model",
		Input:                 nil,
		ContextWindow:         0,
		MaxTokens:             0,
		ReasoningCapabilities: model.ReasoningCapabilities{},
		ToolCapabilities:      model.ToolCapabilities{},
		Pricing:               mo.None[model.Pricing](),
	}
	newModel := model.Descriptor{
		Provider:              "new-provider",
		Model:                 "new-model",
		Input:                 nil,
		ContextWindow:         0,
		MaxTokens:             0,
		ReasoningCapabilities: model.ReasoningCapabilities{},
		ToolCapabilities:      model.ToolCapabilities{},
		Pricing:               mo.None[model.Pricing](),
	}
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
					{
						Kind:            model.ContentReasoning,
						Text:            mo.Some("visible reasoning"),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
					},
					testCallItem(call),
				},
				Outcome: mo.Some(
					model.OutcomeToolUse,
				),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
			}, nil)
		},
	)
	oldProvider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("stale provider used")).
		AnyTimes()
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
				Content: []model.Content{
					testTextItem("done"),
				},
				Outcome:       mo.Some(model.OutcomeStop),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
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
		Outcome: mo.Some(
			model.OutcomeToolUse,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	stop := model.Response{
		Content:       []model.Content{testTextItem("done")},
		Outcome:       mo.Some(model.OutcomeStop),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(toolUse, nil))
	toolErr := errors.New("tool operation failed")
	tools.EXPECT().Execute(gomock.Any(), calls[0], gomock.Any()).Return(
		agent.ToolResult{}, toolErr,
	)
	tools.EXPECT().Execute(gomock.Any(), calls[1], gomock.Any()).Return(
		agent.ToolResult{
			CallID:   calls[1].ID,
			ToolName: calls[1].Name,
			Contents: tool.TextContents("ok"),
			IsError:  false,
		}, nil,
	)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			require.Len(t, request.History, 4)
			assert.Equal(t, "failed", request.History[2].ToolResult.OrEmpty().CallID)
			assert.True(t, request.History[2].ToolResult.OrEmpty().IsError)
			require.ErrorContains(
				t,
				errors.New(request.History[2].ToolResult.OrEmpty().Contents[0].Text.OrEmpty()),
				"tool operation failed",
			)
			assert.Equal(t, "succeeded", request.History[3].ToolResult.OrEmpty().CallID)
			return emitStream(update, stop, nil)
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		hookrunner.New(nil, nil, nil),
		tools,
		events,
	)

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
		Outcome: mo.Some(
			model.OutcomeToolUse,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
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
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		hookrunner.New(nil, nil, nil),
		tools,
		events,
	)

	result, err := service.Run(t.Context(), Request{RunID: "run-progress-error", UserText: "go"})

	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	history := service.History()
	require.Len(t, history, 3)
	assert.True(t, history[2].ToolResult.OrEmpty().IsError)
}
