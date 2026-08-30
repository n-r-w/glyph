package events

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// emptyCoordinatorEvent creates one run event without a variant payload.
func emptyCoordinatorEvent(kind run.EventType, runID string) run.Event {
	return run.Event{
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
		Type:       kind,
		RunID:      runID,
	}
}

// TestCoordinatorOrdersTerminalEventsAndSettlement verifies the Host-owned terminal sequence.
func TestCoordinatorOrdersTerminalEventsAndSettlement(t *testing.T) {
	t.Parallel()

	order := make([]string, 0)
	seenRunIDs := make([]string, 0)
	dispatcher := NewDispatcher(
		func(_ context.Context, event run.Event) error {
			order = append(order, eventName(event.Type))
			seenRunIDs = append(seenRunIDs, event.RunID)
			return nil
		},
		func(_ context.Context, runID string) error {
			order = append(order, "agent_settled")
			seenRunIDs = append(seenRunIDs, runID)
			return nil
		},
	)
	execute := func(ctx context.Context, request run.Request) (run.Result, error) {
		require.Equal(t, "run-fixed", request.RunID)
		require.Equal(t, "request", request.UserText)
		require.NoError(
			t,
			dispatcher.Deliver(
				ctx,
				emptyCoordinatorEvent(run.EventAgentStart, request.RunID),
			),
		)
		require.NoError(
			t,
			dispatcher.Deliver(
				ctx,
				emptyCoordinatorEvent(run.EventAgentEnd, request.RunID),
			),
		)
		return completedResult(), nil
	}
	settle := func(runID string) error {
		order = append(order, "settle")
		seenRunIDs = append(seenRunIDs, runID)
		return nil
	}
	coordinator := newCoordinator(
		execute,
		settle,
		dispatcher,
		func() (string, error) { return "run-fixed", nil },
		newAvailableOperationGate(t),
	)

	outcome, err := coordinator.Run(t.Context(), "request")

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, outcome)
	assert.Equal(t, []string{"agent_start", "agent_end", "settle", "agent_settled"}, order)
	assert.Equal(t, []string{"run-fixed", "run-fixed", "run-fixed", "run-fixed"}, seenRunIDs)
}

// TestCoordinatorSettlesPersistenceFailureWithoutHistory verifies terminal cleanup and gate release after the first append fails.
func TestCoordinatorSettlesPersistenceFailureWithoutHistory(t *testing.T) {
	t.Parallel()

	// Arrange a begun failed run with no durable history and an observable operation-gate release.
	controller := gomock.NewController(t)
	gate := NewMockOperationGate(controller)
	released := 0
	sequence := make([]string, 0, 3)
	gate.EXPECT().TryAcquire().Return(func() {
		released++
		sequence = append(sequence, "release")
	}, true)
	settled := 0
	settle := 0
	coordinator := newCoordinator(
		func(context.Context, run.Request) (run.Result, error) {
			return run.Result{
				Outcome: agent.RunOutcomeFailed, AddedHistory: nil,
				ErrorMessage: mo.Some("session persistence failed"),
			}, run.ErrPersistenceUnavailable
		},
		func(string) error {
			settle++
			sequence = append(sequence, "settle")
			return nil
		},
		NewDispatcher(func(context.Context, run.Event) error { return nil }, func(context.Context, string) error {
			settled++
			sequence = append(sequence, "settled")
			return nil
		}),
		func() (string, error) { return "failed-run", nil },
		gate,
	)

	// Act by running the accepted request through terminal persistence failure.
	outcome, err := coordinator.Run(t.Context(), "request")

	// Assert Agent Core settles, the client receives settlement, and the operation gate releases once.
	require.ErrorIs(t, err, run.ErrPersistenceUnavailable)
	assert.Equal(t, agent.RunOutcomeFailed, outcome)
	assert.Equal(t, 1, settle)
	assert.Equal(t, 1, settled)
	assert.Equal(t, 1, released)
	assert.Equal(t, []string{"settle", "settled", "release"}, sequence)
}

// TestCoordinatorSettlesAfterDeliveryFailures verifies one attempt per terminal step without retry.
func TestCoordinatorSettlesAfterDeliveryFailures(t *testing.T) {
	t.Parallel()

	deliveryErr := errors.New("recipient failed")
	settledErr := errors.New("settled recipient failed")
	settleErr := errors.New("settlement failed")
	updates := 0
	settlements := 0
	dispatcher := NewDispatcher(
		func(_ context.Context, event run.Event) error {
			if event.Type == run.EventTextDelta {
				updates++
				return deliveryErr
			}
			return nil
		},
		func(context.Context, string) error { return settledErr },
	)
	execute := func(ctx context.Context, request run.Request) (run.Result, error) {
		updateErr := dispatcher.Deliver(
			ctx,
			run.Event{
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventTextDelta,
				RunID:      request.RunID,
				Position:   mo.Some(0),
				Content: mo.Some(model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some("partial"),
				}),
			},
		)
		require.ErrorIs(t, updateErr, deliveryErr)
		require.NoError(
			t,
			dispatcher.Deliver(
				ctx,
				emptyCoordinatorEvent(run.EventAgentEnd, request.RunID),
			),
		)
		return failedResult(), updateErr
	}
	settle := func(string) error {
		settlements++
		return settleErr
	}
	coordinator := newCoordinator(
		execute,
		settle,
		dispatcher,
		func() (string, error) { return "run", nil },
		newAvailableOperationGate(t),
	)

	outcome, err := coordinator.Run(t.Context(), "request")

	assert.Equal(t, agent.RunOutcomeFailed, outcome)
	require.ErrorIs(t, err, deliveryErr)
	require.ErrorIs(t, err, settledErr)
	require.ErrorIs(t, err, settleErr)
	assert.Equal(t, 1, updates)
	assert.Equal(t, 1, settlements)
}

// TestCoordinatorSkipsSettlementWhenRunNeverBegins verifies pre-run rejection does not invent events.
func TestCoordinatorSkipsSettlementWhenRunNeverBegins(t *testing.T) {
	t.Parallel()

	settledCalls := 0
	settleCalls := 0
	dispatcher := NewDispatcher(
		func(context.Context, run.Event) error { return nil },
		func(context.Context, string) error {
			settledCalls++
			return nil
		},
	)
	coordinator := newCoordinator(
		func(context.Context, run.Request) (run.Result, error) { return run.Result{}, run.ErrRunActive },
		func(string) error {
			settleCalls++
			return nil
		},
		dispatcher,
		func() (string, error) { return "run", nil },
		newAvailableOperationGate(t),
	)

	_, err := coordinator.Run(t.Context(), "request")

	require.ErrorIs(t, err, run.ErrRunActive)
	assert.Zero(t, settledCalls)
	assert.Zero(t, settleCalls)
}

// TestCoordinatorRunsPreparedIdentifier verifies acceptance-time allocation and shared execution.
func TestCoordinatorRunsPreparedIdentifier(t *testing.T) {
	t.Parallel()

	allocated := 0
	dispatcher := NewDispatcher(
		func(context.Context, run.Event) error { return nil },
		func(context.Context, string) error { return nil },
	)
	coordinator := newCoordinator(
		func(_ context.Context, request run.Request) (run.Result, error) {
			assert.Equal(t, "prepared-run", request.RunID)
			assert.Equal(t, "request", request.UserText)
			return completedResult(), nil
		},
		func(runID string) error {
			assert.Equal(t, "prepared-run", runID)
			return nil
		},
		dispatcher,
		func() (string, error) {
			allocated++
			return "prepared-run", nil
		},
		newAvailableOperationGate(t),
	)

	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	outcome, err := coordinator.RunPrepared(t.Context(), runID, "request")

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, outcome)
	assert.Equal(t, 1, allocated)
}

// TestGenerateRunIDProducesUniqueNonemptyValues verifies Host-owned identifiers without correlation IDs.
func TestGenerateRunIDProducesUniqueNonemptyValues(t *testing.T) {
	t.Parallel()

	first, err := generateRunID()
	require.NoError(t, err)
	second, err := generateRunID()
	require.NoError(t, err)

	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second)
}

// newAvailableOperationGate returns a gate that accepts every test operation.
func newAvailableOperationGate(t *testing.T) *MockOperationGate {
	t.Helper()
	gate := NewMockOperationGate(gomock.NewController(t))
	gate.EXPECT().TryAcquire().AnyTimes().Return(func() {}, true)
	return gate
}

// completedResult identifies a run that entered Agent Core and completed.
func completedResult() run.Result {
	return run.Result{
		Outcome: agent.RunOutcomeCompleted,
		AddedHistory: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("request")),
			},
		},
		ErrorMessage: mo.None[string](),
	}
}

// failedResult identifies a run that entered Agent Core and failed.
func failedResult() run.Result {
	return run.Result{
		Outcome: agent.RunOutcomeFailed,
		AddedHistory: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("request")),
			},
		},
		ErrorMessage: mo.Some("recipient failed"),
	}
}

// eventName identifies the terminal-order subset used by this test.
func eventName(eventType run.EventType) string {
	switch eventType {
	case run.EventAgentStart:
		return "agent_start"
	case run.EventAgentEnd:
		return "agent_end"
	case run.EventTurnStart,
		run.EventMessageStart,
		run.EventContentStart,
		run.EventTextDelta,
		run.EventContentEnd,
		run.EventToolCallStart,
		run.EventToolCallDelta,
		run.EventToolCallEnd,
		run.EventMessageEnd,
		run.EventToolExecutionStart,
		run.EventToolExecutionUpdate,
		run.EventToolExecutionEnd,
		run.EventToolResult,
		run.EventTurnEnd:
		return "other"
	}
	return "other"
}
