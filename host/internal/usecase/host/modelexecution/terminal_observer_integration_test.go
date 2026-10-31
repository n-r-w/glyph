//go:build integration

package modelexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestTerminalObserverFailureSelectsFailedRun checks logical run failure without rewriting canceled provider history.
func TestTerminalObserverFailureSelectsFailedRun(t *testing.T) {
	t.Parallel()
	for _, eventType := range []agent.EventType{agent.EventMessageEnd, agent.EventTurnEnd} {
		for _, owningCancel := range []bool{false, true} {
			t.Run(
				map[agent.EventType]string{
					agent.EventMessageEnd: "message/", agent.EventTurnEnd: "turn/",
				}[eventType]+map[bool]string{false: "provider", true: "owner"}[owningCancel],
				func(t *testing.T) {
					t.Parallel()
					// Arrange a canceled provider completion and an independent current terminal-observer failure.
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
								ReasoningChoice: model.ReasoningChoiceOff,
								History:         request.History,
								Tools:           request.Tools,
							}, Provider: provider}, nil
						})
					tools.EXPECT().Tools().Return(nil)
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					response := model.Response{
						Content:       nil,
						Outcome:       mo.Some(model.OutcomeAborted),
						ErrorMessage:  mo.None[string](),
						Provider:      mo.None[model.ProviderID](),
						Model:         mo.None[model.ID](),
						ResponseModel: mo.None[model.ID](),
						ResponseID:    mo.Some("canceled-provider-response"),
						Usage:         mo.Some(model.Usage{}),
						Diagnostics:   nil,
					}
					provider.EXPECT().
						Stream(gomock.Any(), gomock.Any(), gomock.Any()).
						DoAndReturn(func(_ context.Context, _ agentrun.ModelRequest, handle agentrun.StreamHandler) error {
							require.NoError(
								t,
								handle(
									agentrun.StreamEvent{
										Kind:     agentrun.StreamEventError,
										Position: mo.None[int](),
										Content:  mo.None[model.Content](),
										Delta:    mo.None[string](),
										Preview:  mo.None[model.ToolCallPreview](),
										ToolCall: mo.None[model.ToolCall](),
										Response: mo.Some(response),
									},
								),
							)
							if owningCancel {
								cancel()
							}
							return context.Canceled
						})
					cause := errors.New("terminal observer independent complete cause")
					failure := modelselection.NewExtensionFailure(cause)
					var summary agent.RunSummary
					events.EXPECT().
						Deliver(gomock.Any(), gomock.Any()).
						DoAndReturn(func(deliveryCtx context.Context, event agent.Event) error {
							if event.Type == eventType {
								require.NoError(t, deliveryCtx.Err())
								return failure
							}
							if event.Type == agent.EventAgentEnd {
								summary = event.Agent.OrEmpty()
							}
							return nil
						}).
						AnyTimes()
					core := agentrun.New("", runtime, tools, events, store, passThroughFinalizer(mocks))
					// Act through Core's terminal observation and summary construction.
					result, err := core.Run(
						ctx,
						runcontrol.Request{RunID: "terminal-observer", Message: model.TextMessage("request")},
					)
					// Assert failed logical state, complete causes and unchanged provider outcome and metadata.
					require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
					require.Equal(t, agent.RunOutcomeFailed, summary.Outcome)
					require.ErrorIs(t, err, failure)
					require.ErrorIs(t, err, cause)
					require.ErrorIs(t, err, context.Canceled)
					require.Contains(t, summary.ErrorMessage.OrEmpty(), cause.Error())
					require.Len(t, history, 2)
					retained := history[1].Model.OrEmpty()
					require.Equal(t, model.OutcomeAborted, retained.Outcome.OrEmpty())
					require.Equal(t, response.ResponseID, retained.ResponseID)
					require.Equal(t, response.Usage, retained.Usage)
					require.Equal(t, history, summary.AddedHistory)
					require.NoError(t, core.Settle("terminal-observer"))
					require.Equal(t, agentrun.StatusIdle, core.State().Status)
				},
			)
		}
	}
}
