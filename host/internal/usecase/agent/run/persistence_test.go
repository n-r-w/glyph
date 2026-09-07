//go:build !integration

package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// expectToolResultPersistenceFailure configures history persistence through a failing tool result.
func expectToolResultPersistenceFailure(
	t *testing.T,
	store *MockHistoryStore,
	history *[]agent.HistoryEntry,
	persistenceErr error,
) {
	t.Helper()
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(*history) }).AnyTimes()
	gomock.InOrder(
		store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				*history = append(*history, entry.Clone())
				return nil
			},
		),
		store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				require.Equal(t, agent.HistoryEntryModel, entry.Kind)
				*history = append(*history, entry.Clone())
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
}

// TestServiceRunStopsBeforeProviderWhenUserPersistenceFails verifies user persistence causes reach the terminal result.
func TestServiceRunStopsBeforeProviderWhenUserPersistenceFails(t *testing.T) {
	t.Parallel()

	// Arrange a first-user persistence failure and event capture without provider or tool expectations.
	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := errors.New("/secret/path user-content provider-context")
	store.EXPECT().Snapshot().Return(nil).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).Return(persistErr)
	// agentEnd retains the terminal diagnostic emitted to the Host.
	var agentEnd agent.RunSummary
	observed := make([]agent.EventType, 0, 2)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		observed = append(observed, event.Type)
		if event.Type == agent.EventAgentEnd {
			agentEnd = event.Agent.MustGet()
		}
		return nil
	}).Times(2)
	service := New(testInstructions, runtime, tools, events, store)

	// Act by starting a run whose first durable user entry fails.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "persist-user", UserText: "hello"})

	// Assert the first-append failure still reports its actual settlement transition and complete cause.
	require.True(t, result.SettlementRequired)
	require.ErrorIs(t, err, agent.ErrPersistenceUnavailable)
	require.ErrorIs(t, err, persistErr)
	assert.Equal(t, agent.ErrPersistenceUnavailable.Error()+": "+persistErr.Error(), agentEnd.ErrorMessage.OrEmpty())
	assert.Equal(t, []agent.EventType{agent.EventAgentStart, agent.EventAgentEnd}, observed)
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
			persistenceErr := fmt.Errorf("%w: unique ToolResult persistence failure", agent.ErrPersistenceUnavailable)
			call := model.ToolCall{ID: "combined-tool", Name: "write", Arguments: map[string]any{"path": "output.txt"}}
			response := model.Response{
				Content: []model.Content{testCallItem(call)}, Outcome: mo.Some(model.OutcomeToolUse),
				ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](),
				Diagnostics: nil,
			}
			history := make([]agent.HistoryEntry, 0, 2)
			expectToolResultPersistenceFailure(t, store, &history, persistenceErr)
			runtime.EXPECT().Snapshot().Return(RequestSnapshot{
				Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
			})
			tools.EXPECT().Tools().Return(nil)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
			observed := make([]agent.EventType, 0)
			var agentEnd agent.RunSummary
			events.EXPECT().
				Deliver(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, event agent.Event) error {
					observed = append(observed, event.Type)
					if event.Type == agent.EventAgentEnd {
						agentEnd = event.Agent.OrEmpty()
					}
					if test.progressFailure && event.Type == agent.EventToolExecutionUpdate {
						return progressErr
					}
					return nil
				}).
				AnyTimes()
			tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ model.ToolCall, handleProgress tool.ProgressHandler) (agent.ToolResult, error) {
					if test.progressFailure {
						err := handleProgress(tool.Progress{Channel: tool.ProgressChannelStdout, Content: "partial"})
						require.ErrorIs(t, err, progressErr)
						return agent.ToolResult{
							CallID:   call.ID,
							ToolName: call.Name,
							Contents: tool.TextContents("partial"),
							IsError:  false,
						}, fmt.Errorf("runtime propagated progress: %w", err)
					}
					return agent.ToolResult{}, toolErr
				},
			)
			service := New(testInstructions, runtime, tools, events, store)

			// Act by running until persistence blocks the ToolResult boundary.
			result, err := service.Run(
				t.Context(),
				runcontrol.Request{RunID: "combined-tool-failure", UserText: "write"},
			)

			// Assert every independent cause reaches the run and no dependent work follows persistence failure.
			require.True(t, result.SettlementRequired)
			require.ErrorIs(t, err, persistenceErr)
			expectedPriorErr := toolErr
			if test.progressFailure {
				expectedPriorErr = progressErr
			}
			require.ErrorIs(t, err, expectedPriorErr)
			for _, text := range []string{err.Error(), agentEnd.ErrorMessage.OrEmpty()} {
				assert.Equal(t, 1, strings.Count(text, persistenceErr.Error()), text)
				assert.Equal(t, 1, strings.Count(text, expectedPriorErr.Error()), text)
			}
			assert.True(t, strings.HasPrefix(agentEnd.ErrorMessage.OrEmpty(), agent.ErrPersistenceUnavailable.Error()))
			assert.NotContains(t, observed, agent.EventToolExecutionEnd)
			assert.NotContains(t, observed, agent.EventToolResult)
			require.Len(t, history, 2)
		})
	}
}

// TestServiceRunStopsAfterCompletedToolWhenResultPersistenceFails verifies tool-result persistence causes reach
// the terminal result.
func TestServiceRunStopsAfterCompletedToolWhenResultPersistenceFails(t *testing.T) {
	t.Parallel()

	// Arrange one completed tool effect followed by terminal tool-result persistence failure.
	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	persistErr := errors.New("/secret/path tool-result provider-context")
	call := model.ToolCall{ID: "call", Name: "write", Arguments: map[string]any{"path": "output.txt"}}
	response := model.Response{
		Content: []model.Content{testCallItem(call)}, Outcome: mo.Some(model.OutcomeToolUse),
		ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.Some("response-id"), Usage: mo.None[model.Usage](),
		Diagnostics: nil,
	}
	history := make([]agent.HistoryEntry, 0, 2)
	expectToolResultPersistenceFailure(t, store, &history, persistErr)
	runtime.EXPECT().Snapshot().Return(RequestSnapshot{
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
				CallID:   call.ID,
				ToolName: call.Name,
				Contents: tool.TextContents("external effect complete"),
				IsError:  false,
			}, nil
		},
	)
	// agentEnd retains the terminal diagnostic emitted to the Host.
	var agentEnd agent.RunSummary
	observed := make([]agent.EventType, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		observed = append(observed, event.Type)
		if event.Type == agent.EventAgentEnd {
			agentEnd = event.Agent.MustGet()
		}
		return nil
	}).AnyTimes()
	service := New(testInstructions, runtime, tools, events, store)

	// Act by running through one completed tool invocation whose result cannot become durable.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "persist-tool", UserText: "write"})
	require.True(t, result.SettlementRequired)

	// Assert the external effect remains complete and the persistence cause reaches the terminal result.
	require.ErrorIs(t, err, agent.ErrPersistenceUnavailable)
	require.ErrorIs(t, err, persistErr)
	assert.Equal(t, agent.ErrPersistenceUnavailable.Error()+"\n"+persistErr.Error(), agentEnd.ErrorMessage.OrEmpty())
	require.True(t, toolCompleted)
	assert.NotContains(t, observed, agent.EventToolExecutionEnd)
	assert.NotContains(t, observed, agent.EventToolResult)
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
	persistErr := errors.New("/secret/path model-content provider-context")
	history := make([]agent.HistoryEntry, 0, 1)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(history) }).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, entry.Clone())
			return nil
		},
	)
	store.EXPECT().Append(gomock.Any(), gomock.Any()).Return(persistErr)
	runtime.EXPECT().Snapshot().Return(RequestSnapshot{
		Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
	})
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			return handle(StreamEvent{
				Kind:     StreamEventDone,
				Position: mo.None[int](),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.Some(model.Response{
					Content: []model.Content{testTextItem("done")}, Outcome: mo.Some(model.OutcomeStop),
					ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](),
					Diagnostics: nil,
				}),
			})
		},
	)
	// agentEnd retains the terminal diagnostic emitted to the Host.
	var agentEnd agent.RunSummary
	observed := make([]agent.EventType, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		observed = append(observed, event.Type)
		if event.Type == agent.EventAgentEnd {
			agentEnd = event.Agent.MustGet()
		}
		return nil
	}).AnyTimes()
	service := New(testInstructions, runtime, tools, events, store)

	// Act by completing a provider response that cannot become durable.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "persist-model", UserText: "hello"})
	require.True(t, result.SettlementRequired)

	// Assert no terminal model event escapes and the persistence cause reaches the terminal result.
	require.ErrorIs(t, err, agent.ErrPersistenceUnavailable)
	require.ErrorIs(t, err, persistErr)
	assert.Equal(t, agent.ErrPersistenceUnavailable.Error()+": "+persistErr.Error(), agentEnd.ErrorMessage.OrEmpty())
	assert.NotContains(t, observed, agent.EventMessageEnd)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
}
