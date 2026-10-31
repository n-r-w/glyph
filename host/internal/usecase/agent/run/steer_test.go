//go:build !integration

package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// newStopProvider returns a provider that records each request history and always gives a final response.
func newStopProvider(t *testing.T, histories *[][]agent.HistoryEntry) *MockModelProvider {
	t.Helper()
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			*histories = append(*histories, request.History)
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).AnyTimes()
	return provider
}

// newNoTools returns a tool runtime without tools.
func newNoTools(t *testing.T) *MockToolRuntime {
	t.Helper()
	tools := NewMockToolRuntime(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil).AnyTimes()
	return tools
}

// userTexts returns the text of each user entry in history order.
func userTexts(history []agent.HistoryEntry) []string {
	texts := make([]string, 0, len(history))
	for _, entry := range history {
		if message, present := entry.User.Get(); present {
			texts = append(texts, message.Content[0].Text.OrEmpty())
		}
	}
	return texts
}

// TestSteerAtRunCompletion checks that a steer message accepted at the end of the final turn starts another
// model request, and that Agent Core rejects a steer message after the run leaves the running state.
func TestSteerAtRunCompletion(t *testing.T) {
	t.Parallel()
	// Arrange events that queue one message at the first turn end and another one at agent end.
	var histories [][]agent.HistoryEntry
	var service *Service
	var lateErr error
	turnEnds := 0
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		switch event.Type {
		case agent.EventTurnEnd:
			turnEnds++
			if turnEnds == 1 {
				require.NoError(t, service.Steer("run", model.TextMessage("steer")))
			}
		case agent.EventAgentEnd:
			lateErr = service.Steer("run", model.TextMessage("late"))
		default:
		}
		return nil
	}).AnyTimes()
	service = newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
		newStopProvider(t, &histories), newNoTools(t), events,
	)

	// Act by running one request whose first final response is followed by the steer message.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert the second model request contains the steer message, and the late message was rejected.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, histories, 2)
	require.Equal(t, []string{"request"}, userTexts(histories[0]))
	require.Equal(t, []string{"request", "steer"}, userTexts(histories[1]))
	require.ErrorIs(t, lateErr, agent.ErrRunNotActive)
	require.NoError(t, service.Settle("run"))
	require.ErrorIs(t, service.Steer("run", model.TextMessage("idle")), agent.ErrRunNotActive)
}

// TestSteerDiscardedWhenRunFails checks that a steer message pending when a run fails reaches no later run.
func TestSteerDiscardedWhenRunFails(t *testing.T) {
	t.Parallel()
	// Arrange a provider whose first request queues a steer message and fails.
	var histories [][]agent.HistoryEntry
	var service *Service
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			histories = append(histories, request.History)
			if len(histories) == 1 {
				require.NoError(t, service.Steer("failed", model.TextMessage("stale")))
				return emitStream(handle, emptyModelResponse(model.OutcomeFailed), nil)
			}
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).Times(2)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff, provider,
		newNoTools(t), events)

	// Act by running the failing request, settling it, and running another request.
	failed, failedErr := service.Run(
		t.Context(),
		runcontrol.Request{RunID: "failed", Message: model.TextMessage("one")},
	)
	require.NoError(t, service.Settle("failed"))
	completed, err := service.Run(t.Context(), runcontrol.Request{RunID: "next", Message: model.TextMessage("two")})

	// Assert the failed run ended, and the next run made one model request without the stale message.
	require.Error(t, failedErr)
	require.Equal(t, agent.RunOutcomeFailed, failed.Outcome)
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, completed.Outcome)
	require.Len(t, histories, 2)
	require.NotContains(t, userTexts(service.History()), "stale")
}

// TestSteerRejectsEarlierRun checks that a message checked against one run is rejected while a later run is running.
func TestSteerRejectsEarlierRun(t *testing.T) {
	t.Parallel()
	// Arrange a provider that checks steer admission in the first run and queues for that run in the second run.
	var histories [][]agent.HistoryEntry
	var service *Service
	var checkedID, secondID string
	var staleErr error
	provider := NewMockModelProvider(gomock.NewController(t))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			histories = append(histories, request.History)
			var err error
			if len(histories) == 1 {
				checkedID, err = service.CheckSteer()
			} else {
				secondID, err = service.CheckSteer()
				staleErr = service.Steer(checkedID, model.TextMessage("stale"))
			}
			require.NoError(t, err)
			return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
		},
	).Times(2)
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff, provider,
		newNoTools(t), events)

	// Act by running and settling the first request, then running the second request.
	_, err := service.Run(t.Context(), runcontrol.Request{RunID: "first", Message: model.TextMessage("one")})
	require.NoError(t, err)
	require.NoError(t, service.Settle("first"))
	_, err = service.Run(t.Context(), runcontrol.Request{RunID: "second", Message: model.TextMessage("two")})

	// Assert each check named its own run, and the second run rejected and never received the stale message.
	require.NoError(t, err)
	require.Equal(t, "first", checkedID)
	require.Equal(t, "second", secondID)
	require.ErrorIs(t, staleErr, agent.ErrRunNotActive)
	require.Len(t, histories, 2)
	require.NotContains(t, userTexts(service.History()), "stale")
}
