//nolint:exhaustruct // Tests set only active event and history union fields.
package events

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

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
		require.NoError(t, dispatcher.Deliver(ctx, run.Event{Type: run.EventAgentStart, RunID: request.RunID}))
		require.NoError(t, dispatcher.Deliver(ctx, run.Event{Type: run.EventAgentEnd, RunID: request.RunID}))
		return completedResult(), nil
	}
	settle := func(runID string) error {
		order = append(order, "settle")
		seenRunIDs = append(seenRunIDs, runID)
		return nil
	}
	coordinator := newCoordinator(execute, settle, dispatcher, func() (string, error) { return "run-fixed", nil })

	outcome, err := coordinator.Run(t.Context(), "request")

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, outcome)
	assert.Equal(t, []string{"agent_start", "agent_end", "agent_settled", "settle"}, order)
	assert.Equal(t, []string{"run-fixed", "run-fixed", "run-fixed", "run-fixed"}, seenRunIDs)
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
			if event.Type == run.EventMessageUpdate {
				updates++
				return deliveryErr
			}
			return nil
		},
		func(context.Context, string) error { return settledErr },
	)
	execute := func(ctx context.Context, request run.Request) (run.Result, error) {
		updateErr := dispatcher.Deliver(ctx, run.Event{
			Type: run.EventMessageUpdate, RunID: request.RunID, Position: 0, Delta: "partial",
		})
		require.ErrorIs(t, updateErr, deliveryErr)
		require.NoError(t, dispatcher.Deliver(ctx, run.Event{Type: run.EventAgentEnd, RunID: request.RunID}))
		return failedResult(), updateErr
	}
	settle := func(string) error {
		settlements++
		return settleErr
	}
	coordinator := newCoordinator(execute, settle, dispatcher, func() (string, error) { return "run", nil })

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
	)

	_, err := coordinator.Run(t.Context(), "request")

	require.ErrorIs(t, err, run.ErrRunActive)
	assert.Zero(t, settledCalls)
	assert.Zero(t, settleCalls)
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

// completedResult identifies a run that entered Agent Core and completed.
func completedResult() run.Result {
	return run.Result{
		Outcome:      agent.RunOutcomeCompleted,
		AddedHistory: []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "request"}}},
		ErrorMessage: "",
	}
}

// failedResult identifies a run that entered Agent Core and failed.
func failedResult() run.Result {
	return run.Result{
		Outcome:      agent.RunOutcomeFailed,
		AddedHistory: []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "request"}}},
		ErrorMessage: "recipient failed",
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
		run.EventMessageUpdate,
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
