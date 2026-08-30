package run

import (
	"context"
	"errors"

	"strings"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestServiceRunMixedProviderCancellationPreservesIndependentDetail verifies mixed cancellation presentation.
func TestServiceRunMixedProviderCancellationPreservesIndependentDetail(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mixed bool
	}{
		"pure cancellation":   {mixed: false},
		"independent sibling": {mixed: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a provider cancellation with an optional independent sibling and no response text.
			provider := NewMockModelProvider(gomock.NewController(t))
			tools := NewMockToolRuntime(gomock.NewController(t))
			events := NewMockEventSink(gomock.NewController(t))
			independentErr := errors.New("unique mixed cancellation sibling")
			var providerErr error = context.Canceled
			if test.mixed {
				providerErr = errors.Join(context.Canceled, independentErr)
			}
			tools.EXPECT().Tools().Return(nil)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
					return emitStream(handle, emptyModelResponse(model.OutcomeAborted), providerErr)
				},
			)
			var messageEnd model.Response
			var agentEnd AgentSummary
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
				if event.Type == EventMessageEnd {
					messageEnd = event.Message.OrEmpty()
				}
				if event.Type == EventAgentEnd {
					agentEnd = event.Agent.OrEmpty()
				}
				return nil
			}).AnyTimes()
			service := newTestService(
				t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh,
				provider, hookrunner.New(nil, nil, nil), tools, events,
			)

			// Act by finalizing the provider cancellation.
			result, err := service.Run(t.Context(), Request{RunID: "mixed-provider-cancel", UserText: "cancel"})

			// Assert outcome and error classifications stay stable while terminal text filters cancellation leaves.
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, agent.RunOutcomeAborted, result.Outcome)
			if !test.mixed {
				assert.Equal(t, abortedModelMessage, result.ErrorMessage.OrEmpty())
				assert.Equal(t, abortedModelMessage, messageEnd.ErrorMessage.OrEmpty())
				assert.Equal(t, abortedModelMessage, agentEnd.ErrorMessage.OrEmpty())
				return
			}
			require.ErrorIs(t, err, independentErr)
			for _, text := range []string{
				result.ErrorMessage.OrEmpty(), messageEnd.ErrorMessage.OrEmpty(), agentEnd.ErrorMessage.OrEmpty(),
			} {
				assert.Equal(t, 1, strings.Count(text, independentErr.Error()), text)
				assert.NotContains(t, text, abortedModelMessage)
			}
		})
	}
}

// TestServiceRunCancellationWithTerminalFailuresPreservesNonCancellationCauses verifies cancellation filtering.
func TestServiceRunCancellationWithTerminalFailuresPreservesNonCancellationCauses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		failure string
	}{
		"pure cancellation":    {failure: ""},
		"terminal validation":  {failure: "validation"},
		"terminal persistence": {failure: "persistence"},
		"terminal delivery":    {failure: "delivery"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange provider cancellation with an optional independent terminal failure.
			controller := gomock.NewController(t)
			runtime := NewMockModelRuntime(controller)
			provider := NewMockModelProvider(controller)
			tools := NewMockToolRuntime(controller)
			events := NewMockEventSink(controller)
			store := NewMockHistoryStore(controller)
			siblingErr := errors.New("unique " + test.failure + " failure")
			history := make([]agent.HistoryEntry, 0, 2)
			modelAppendCalls := 0
			store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(history) }).AnyTimes()
			store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, entry agent.HistoryEntry) error {
					if entry.Kind == agent.HistoryEntryModel {
						modelAppendCalls++
						if test.failure == "persistence" {
							return siblingErr
						}
					}
					history = append(history, cloneHistoryEntry(entry))
					return nil
				},
			).AnyTimes()
			runtime.EXPECT().Current().Return(RuntimeSelection{
				Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
			})
			tools.EXPECT().Tools().Return(nil)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
					if test.failure == "validation" {
						require.NoError(t, handle(testTextStreamEvent(
							StreamEventContentStart, 1, model.ContentText, "", mo.None[string](),
						)))
						return context.Canceled
					}
					return emitStream(handle, model.Response{
						Content: nil, Outcome: mo.Some(model.OutcomeAborted), ErrorMessage: mo.None[string](),
						Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
						ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
					}, context.Canceled)
				},
			)
			var agentEnd AgentSummary
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
				if event.Type == EventAgentEnd {
					agentEnd = event.Agent.OrEmpty()
				}
				if test.failure == "delivery" && event.Type == EventMessageEnd {
					return siblingErr
				}
				return nil
			}).AnyTimes()
			service := New(testInstructions, runtime, hookrunner.New(nil, nil, nil), tools, events, store)

			// Act by running the canceled provider through terminal finalization.
			result, err := service.Run(t.Context(), Request{RunID: "cancellation-terminal-failure", UserText: "cancel"})

			// Assert pure cancellation is canonical and combined failures expose only non-cancellation detail.
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, agent.RunOutcomeAborted, result.Outcome)
			if test.failure == "" {
				assert.Equal(t, abortedModelMessage, result.ErrorMessage.OrEmpty())
				assert.Equal(t, abortedModelMessage, agentEnd.ErrorMessage.OrEmpty())
				assert.Equal(t, 1, modelAppendCalls)
				return
			}
			if test.failure == "validation" {
				assert.Contains(t, err.Error(), "unknown kind")
				assert.Contains(t, result.ErrorMessage.OrEmpty(), "unknown kind")
				assert.Contains(t, agentEnd.ErrorMessage.OrEmpty(), "unknown kind")
				assert.Zero(t, modelAppendCalls)
				return
			}
			require.ErrorIs(t, err, siblingErr)
			assert.Contains(t, result.ErrorMessage.OrEmpty(), siblingErr.Error())
			assert.Contains(t, agentEnd.ErrorMessage.OrEmpty(), siblingErr.Error())
			assert.Equal(t, 1, modelAppendCalls)
		})
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
