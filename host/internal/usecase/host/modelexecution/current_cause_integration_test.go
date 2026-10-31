//go:build integration

package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestCoreCurrentCancellationRetainsDiagnostics checks response selection and later terminal failure semantics.
func TestCoreCurrentCancellationRetainsDiagnostics(t *testing.T) {
	t.Parallel()
	recovered := errors.New("recovered provider detail")
	boundary := diagnostic.NewCurrentCause(context.Canceled, recovered)
	independent := errors.New("independent terminal detail")
	classified := &LogicalFailureError{Category: FailureExtensionFailed, Cause: context.Canceled}
	for _, scenario := range []struct {
		name    string
		cause   error
		outcome agent.RunOutcome
		later   bool
	}{
		{name: "recovered cancellation", cause: boundary, outcome: agent.RunOutcomeAborted, later: false},
		{
			name: "ordinary wrapper", cause: fmt.Errorf("logical stream: %w", boundary),
			outcome: agent.RunOutcomeAborted, later: false,
		},
		{
			name: "cancellation-only recovered text",
			cause: diagnostic.NewCurrentCause(
				context.Canceled, fmt.Errorf("recovered canceled attempt: %w", context.Canceled),
			),
			outcome: agent.RunOutcomeAborted, later: false,
		},
		{
			name: "independent before", cause: errors.Join(independent, boundary),
			outcome: agent.RunOutcomeFailed, later: false,
		},
		{name: "independent after", cause: errors.Join(boundary, independent), outcome: agent.RunOutcomeFailed, later: false},
		{
			name: "classified enclosing", cause: &LogicalFailureError{Category: FailureExtensionFailed, Cause: boundary},
			outcome: agent.RunOutcomeFailed, later: false,
		},
		{name: "classified sibling", cause: errors.Join(boundary, classified), outcome: agent.RunOutcomeFailed, later: false},
		{name: "late terminal delivery", cause: boundary, outcome: agent.RunOutcomeAborted, later: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a canceled logical stream with recovered diagnostics and an optional later delivery failure.
			mocks := gomock.NewController(t)
			runtime := agentrun.NewMockEffectiveContextPort(mocks)
			provider := agentrun.NewMockModelProvider(mocks)
			tools := agentrun.NewMockToolRuntime(mocks)
			events := agentrun.NewMockEventSink(mocks)
			store := agentrun.NewMockHistoryStore(mocks)
			history := make([]agent.HistoryEntry, 0, 2)
			store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return history }).AnyTimes()
			store.EXPECT().
				Append(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, entry agent.HistoryEntry) error {
					history = append(history, entry.Clone())
					return nil
				}).
				AnyTimes()
			runtime.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, request agentrun.ContextRequest) (agentrun.EffectiveContext, error) {
					return agentrun.EffectiveContext{Request: agentrun.ModelRequest{
						Instructions:    request.Instructions,
						Model:           model.Descriptor{},
						ReasoningChoice: model.ReasoningChoiceHigh,
						History:         request.History,
						Tools:           request.Tools,
					}, Provider: provider}, nil
				})
			tools.EXPECT().Tools().Return(nil)
			response := model.Response{
				Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
				Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
				ResponseID: mo.Some("retained-response"), Usage: mo.Some(model.Usage{}), Diagnostics: nil,
			}
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ agentrun.ModelRequest, handle agentrun.StreamHandler) error {
					require.NoError(t, handle(agentrun.StreamEvent{
						Kind:     agentrun.StreamEventError,
						Position: mo.None[int](),
						Content:  mo.None[model.Content](),
						Delta:    mo.None[string](),
						Preview:  mo.None[model.ToolCallPreview](),
						ToolCall: mo.None[model.ToolCall](),
						Response: mo.Some(response),
					}))
					return scenario.cause
				},
			)
			var messageEnd model.Response
			var agentEnd agent.RunSummary
			events.EXPECT().
				Deliver(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, event agent.Event) error {
					if event.Type == agent.EventMessageEnd {
						messageEnd = event.Message.OrEmpty()
						if scenario.later {
							return independent
						}
					}
					if event.Type == agent.EventAgentEnd {
						agentEnd = event.Agent.OrEmpty()
					}
					return nil
				}).
				AnyTimes()
			core := agentrun.New("", runtime, tools, events, store, passThroughFinalizer(mocks))
			// Act through Core response selection, persistence and event delivery.
			result, err := core.Run(
				t.Context(),
				runcontrol.Request{RunID: "current-causes", Message: model.TextMessage("request")},
			)
			// Assert diagnostics survive without retrospectively changing selected response outcomes.
			require.Equal(t, scenario.outcome, result.Outcome)
			require.ErrorIs(t, err, context.Canceled)
			require.Contains(t, messageEnd.ErrorMessage.OrEmpty(), scenario.cause.Error())
			require.Contains(t, agentEnd.ErrorMessage.OrEmpty(), scenario.cause.Error())
			require.Len(t, history, 2)
			require.Equal(t, messageEnd, history[1].Model.OrEmpty())
			require.Equal(t, response.ResponseID, messageEnd.ResponseID)
			require.Equal(t, response.Usage, messageEnd.Usage)
			if scenario.later {
				require.ErrorIs(t, err, independent)
				require.Contains(t, agentEnd.ErrorMessage.OrEmpty(), independent.Error())
			}
		})
	}
}

// passThroughFinalizer returns an Agent Core finalizer whose final values equal the given values.
func passThroughFinalizer(controller *gomock.Controller) *agentrun.MockMessageFinalizer {
	finalizer := agentrun.NewMockMessageFinalizer(controller)
	finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response model.Response) (model.Response, error) { return response, nil },
	).AnyTimes()
	finalizer.EXPECT().FinalizeToolResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, result agent.ToolResult) (agent.ToolResult, error) { return result, nil },
	).AnyTimes()
	return finalizer
}
