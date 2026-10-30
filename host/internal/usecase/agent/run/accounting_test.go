//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestRunSummaryAfterHistoryReplacement checks owned appends across terminal paths and settlement.
func TestRunSummaryAfterHistoryReplacement(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{
		"completed", "provider-failure", "cancellation", "persistence", "delivery",
		"tool-failure", "tool-persistence", "terminal-delivery",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange a replaceable prepared snapshot and captured owned entries.
			controller := gomock.NewController(t)
			store := NewMockHistoryStore(controller)
			provider := NewMockModelProvider(controller)
			runtime := NewMockEffectiveContextPort(controller)
			tools := NewMockToolRuntime(controller)
			events := NewMockEventSink(controller)
			prepared := make([]agent.HistoryEntry, 10)
			var owned []agent.HistoryEntry
			var summary agent.RunSummary
			cause := errors.New("terminal accounting cause")
			store.EXPECT().
				Snapshot().
				DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(prepared) }).
				AnyTimes()
			store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, entry agent.HistoryEntry) error {
					if (scenario == "persistence" && len(owned) == 1) ||
						(scenario == "tool-persistence" && entry.Kind == agent.HistoryEntryToolResult) {
						return cause
					}
					owned = append(owned, entry.Clone())
					prepared = append(prepared, entry.Clone())
					return nil
				},
			).AnyTimes()
			runtime.EXPECT().
				PrepareContext(gomock.Any(), gomock.Any()).
				DoAndReturn(passThroughContext(testModelDescriptor, "", provider)).
				AnyTimes()
			tools.EXPECT().Tools().Return(nil).AnyTimes()
			calls := 0
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
					prepared = nil
					calls++
					response := emptyModelResponse(model.OutcomeStop)
					if (scenario == "tool-failure" || scenario == "tool-persistence") && calls == 1 {
						response.Outcome = mo.Some(model.OutcomeToolUse)
						response.Content = []model.Content{testCallItem(model.ToolCall{
							ID: "call", Name: "test", Arguments: model.ToolCallArguments{},
						})}
					}
					if scenario == "provider-failure" {
						return emitStream(handle, emptyModelResponse(model.OutcomeFailed), cause)
					}
					if scenario == "cancellation" {
						return emitStream(handle, emptyModelResponse(model.OutcomeAborted), context.Canceled)
					}
					return emitStream(handle, response, nil)
				},
			).AnyTimes()
			if scenario == "tool-failure" || scenario == "tool-persistence" {
				tools.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any()).Return(agent.ToolResult{}, cause)
			}
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, event agent.Event) error {
					if event.Type == agent.EventAgentEnd {
						summary = event.Agent.MustGet()
					}
					if (scenario == "delivery" && event.Type == agent.EventMessageEnd) ||
						(scenario == "terminal-delivery" && event.Type == agent.EventAgentEnd) {
						return cause
					}
					return nil
				},
			).AnyTimes()
			service := New(testInstructions, runtime, tools, events, store)

			// Act by replacing prepared history during provider work and completing the run.
			result, err := service.Run(t.Context(), runcontrol.Request{RunID: "replaced", UserText: "request"})

			// Assert append order and terminal causes survive replacement and explicit settlement.
			require.Equal(t, owned, summary.AddedHistory)
			require.True(t, result.SettlementRequired)
			require.Equal(t, StatusAwaitingSettlement, service.State().Status)
			switch scenario {
			case "provider-failure", "persistence", "delivery":
				require.ErrorIs(t, err, cause)
				require.Contains(t, summary.ErrorMessage.MustGet(), cause.Error())
				require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			case "cancellation":
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
			case "tool-persistence":
				require.ErrorIs(t, err, cause)
				require.ErrorIs(t, err, agent.ErrPersistenceUnavailable)
				require.Contains(t, summary.ErrorMessage.MustGet(), cause.Error())
				require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			case "terminal-delivery":
				require.ErrorIs(t, err, cause)
				require.True(t, summary.ErrorMessage.IsNone())
				require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
			case "completed", "tool-failure":
				require.NoError(t, err)
				require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
			}
			if scenario == "persistence" {
				require.ErrorIs(t, err, agent.ErrPersistenceUnavailable)
				require.Len(t, summary.AddedHistory, 1)
			} else if scenario == "tool-failure" {
				require.Len(t, summary.AddedHistory, 4)
				require.Equal(
					t,
					tool.TextContents(cause.Error()),
					summary.AddedHistory[2].ToolResult.MustGet().Contents,
				)
			} else {
				require.Len(t, summary.AddedHistory, 2)
			}
			if scenario == "terminal-delivery" {
				require.Equal(t, agent.RunOutcomeCompleted, summary.Outcome)
			} else {
				require.Equal(t, result.Outcome, summary.Outcome)
			}
			require.NoError(t, service.Settle("replaced"))
			require.Equal(t, StatusIdle, service.State().Status)
		})
	}
}

// TestRunSummaryOwnsDetachedEntries checks storage ownership and accounting isolation between runs.
func TestRunSummaryOwnsDetachedEntries(t *testing.T) {
	t.Parallel()
	// Arrange a history owner that retains mutable entries and inserts its own prepared entry.
	controller := gomock.NewController(t)
	store := NewMockHistoryStore(controller)
	events := NewMockEventSink(controller)
	var prepared []agent.HistoryEntry
	var transferred []agent.HistoryEntry
	var summaries []agent.RunSummary
	var service *Service
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(prepared) }).AnyTimes()
	store.EXPECT().
		Append(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, entry agent.HistoryEntry) error {
			if response, present := entry.Model.Get(); present {
				response.Content[0].Text = mo.Some("owner mutation")
			}
			transferred = append(transferred, entry)
			prepared = append(prepared, entry)
			return nil
		}).
		AnyTimes()
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventMessageEnd {
			rejected, err := service.Run(t.Context(), runcontrol.Request{RunID: "rejected", UserText: "blocked"})
			require.ErrorIs(t, err, ErrRunActive)
			require.False(t, rejected.SettlementRequired)
			require.Equal(t, StatusRunning, service.State().Status)
			require.Equal(t, event.RunID, service.State().RunID.MustGet())
			prepared = append(prepared, agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("external")),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if event.Type == agent.EventAgentEnd {
			summaries = append(summaries, event.Agent.MustGet())
		}
		return nil
	}).AnyTimes()
	provider := NewMockModelProvider(controller)
	response := emptyModelResponse(model.OutcomeStop)
	response.Content = []model.Content{testTextItem("response")}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil)).Times(2)
	runtime := NewMockEffectiveContextPort(controller)
	runtime.EXPECT().
		PrepareContext(gomock.Any(), gomock.Any()).
		DoAndReturn(passThroughContext(testModelDescriptor, "", provider)).
		Times(2)
	tools := NewMockToolRuntime(controller)
	tools.EXPECT().Tools().Return(nil).Times(2)
	service = New(testInstructions, runtime, tools, events, store)

	// Act through two runs with rejected admission during each run and settlement between them.
	for _, runID := range []string{"first", "second"} {
		_, err := service.Run(t.Context(), runcontrol.Request{RunID: runID, UserText: runID})
		require.NoError(t, err)
		require.NoError(t, service.Settle(runID))
	}

	// Assert each summary contains only its own entries and owns its nested mutable content.
	require.Len(t, summaries, 2)
	for index, runID := range []string{"first", "second"} {
		require.Len(t, summaries[index].AddedHistory, 2)
		require.Equal(t, model.TextMessage(runID), summaries[index].AddedHistory[0].User.MustGet())
		require.Equal(t, "response", summaries[index].AddedHistory[1].Model.MustGet().Content[0].Text.MustGet())
	}
	first := summaries[0].AddedHistory[1].Model.MustGet()
	first.Content[0].Text = mo.Some("summary mutation")
	require.Equal(t, "owner mutation", transferred[1].Model.MustGet().Content[0].Text.MustGet())
	require.Equal(t, "response", summaries[1].AddedHistory[1].Model.MustGet().Content[0].Text.MustGet())
}
