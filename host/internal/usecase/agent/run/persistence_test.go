package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// TestServiceRunStopsBeforeProviderWhenUserPersistenceFails verifies user persistence causes reach the terminal result.
func TestServiceRunStopsBeforeProviderWhenUserPersistenceFails(t *testing.T) {
	t.Parallel()

	// Arrange a first-user persistence failure and event capture without provider or tool expectations.
	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := fmt.Errorf("%w: /secret/path user-content provider-context", ErrPersistenceUnavailable)
	store.EXPECT().Snapshot().Return(nil).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).Return(persistErr)
	observed := make([]EventType, 0, 2)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		observed = append(observed, event.Type)
		return nil
	}).Times(2)
	service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, store)

	// Act by starting a run whose first durable user entry fails.
	result, err := service.Run(t.Context(), Request{RunID: "persist-user", UserText: "hello"})

	// Assert the persistence cause reaches the terminal result before settlement.
	require.ErrorIs(t, err, ErrPersistenceUnavailable)
	assert.Equal(t, persistErr.Error(), result.ErrorMessage.OrEmpty())
	assert.Equal(t, []EventType{EventAgentStart, EventAgentEnd}, observed)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

// TestServiceRunToolFailureAndPersistenceFailurePreservesCauses verifies blocked ToolResults retain prior failures.
func TestServiceRunToolFailureAndPersistenceFailurePreservesCauses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		progressFailure bool
	}{
		"tool execution":    {progressFailure: false},
		"progress delivery": {progressFailure: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a tool or progress failure followed by ToolResult persistence failure.
			controller := gomock.NewController(t)
			runtime := NewMockModelRuntime(controller)
			provider := NewMockModelProvider(controller)
			tools := NewMockToolRuntime(controller)
			events := NewMockEventSink(controller)
			store := NewMockHistoryStore(controller)
			toolErr := errors.New("unique tool execution failure")
			progressErr := errors.New("unique progress delivery failure")
			persistenceErr := fmt.Errorf("%w: unique ToolResult persistence failure", ErrPersistenceUnavailable)
			call := model.ToolCall{ID: "combined-tool", Name: "write", Arguments: map[string]any{"path": "output.txt"}}
			response := model.Response{
				Content: []model.Content{testCallItem(call)}, Outcome: mo.Some(model.OutcomeToolUse),
				ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](),
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
						return persistenceErr
					},
				),
			)
			runtime.EXPECT().Current().Return(RuntimeSelection{
				Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
			})
			tools.EXPECT().Tools().Return(nil)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
			observed := make([]EventType, 0)
			var agentEnd AgentSummary
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
				observed = append(observed, event.Type)
				if event.Type == EventAgentEnd {
					agentEnd = event.Agent.OrEmpty()
				}
				if test.progressFailure && event.Type == EventToolExecutionUpdate {
					return progressErr
				}
				return nil
			}).AnyTimes()
			tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ model.ToolCall, handleProgress tool.ProgressHandler) (agent.ToolResult, error) {
					if test.progressFailure {
						err := handleProgress(tool.Progress{Channel: tool.ProgressChannelStdout, Content: "partial"})
						require.ErrorIs(t, err, progressErr)
						return agent.ToolResult{
							CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("partial"), IsError: false,
						}, fmt.Errorf("runtime propagated progress: %w", err)
					}
					return agent.ToolResult{}, toolErr
				},
			)
			service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, store)

			// Act by running until persistence blocks the ToolResult boundary.
			result, err := service.Run(t.Context(), Request{RunID: "combined-tool-failure", UserText: "write"})

			// Assert every independent cause reaches the run and no dependent work follows persistence failure.
			require.ErrorIs(t, err, persistenceErr)
			expectedPriorErr := toolErr
			if test.progressFailure {
				expectedPriorErr = progressErr
			}
			require.ErrorIs(t, err, expectedPriorErr)
			for _, text := range []string{err.Error(), result.ErrorMessage.OrEmpty(), agentEnd.ErrorMessage.OrEmpty()} {
				assert.Equal(t, 1, strings.Count(text, persistenceErr.Error()), text)
				assert.Equal(t, 1, strings.Count(text, expectedPriorErr.Error()), text)
			}
			for _, text := range []string{result.ErrorMessage.OrEmpty(), agentEnd.ErrorMessage.OrEmpty()} {
				assert.True(t, strings.HasPrefix(text, ErrPersistenceUnavailable.Error()), text)
			}
			assert.NotContains(t, observed, EventToolExecutionEnd)
			assert.NotContains(t, observed, EventToolResult)
			require.Len(t, history, 2)
		})
	}
}

// TestServiceRunStopsAfterCompletedToolWhenResultPersistenceFails verifies tool-result persistence causes reach the terminal result.
func TestServiceRunStopsAfterCompletedToolWhenResultPersistenceFails(t *testing.T) {
	t.Parallel()

	// Arrange one completed tool effect followed by terminal tool-result persistence failure.
	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := fmt.Errorf("%w: /secret/path tool-result provider-context", ErrPersistenceUnavailable)
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

	// Act by running through one completed tool invocation whose result cannot become durable.
	result, err := service.Run(t.Context(), Request{RunID: "persist-tool", UserText: "write"})

	// Assert the external effect remains complete and the persistence cause reaches the terminal result.
	require.ErrorIs(t, err, ErrPersistenceUnavailable)
	assert.Equal(t, persistErr.Error(), result.ErrorMessage.OrEmpty())
	require.True(t, toolCompleted)
	assert.NotContains(t, observed, EventToolExecutionEnd)
	assert.NotContains(t, observed, EventToolResult)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}

// TestServiceRunHidesMessageEndWhenModelPersistenceFails verifies model persistence causes reach the terminal result.
func TestServiceRunHidesMessageEndWhenModelPersistenceFails(t *testing.T) {
	t.Parallel()

	// Arrange a terminal model persistence failure after one provider response.
	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := fmt.Errorf("%w: /secret/path model-content provider-context", ErrPersistenceUnavailable)
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

	// Act by completing a provider response that cannot become durable.
	result, err := service.Run(t.Context(), Request{RunID: "persist-model", UserText: "hello"})

	// Assert no terminal model event escapes and the persistence cause reaches the terminal result.
	require.ErrorIs(t, err, ErrPersistenceUnavailable)
	assert.Equal(t, persistErr.Error(), result.ErrorMessage.OrEmpty())
	assert.NotContains(t, observed, EventMessageEnd)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}
