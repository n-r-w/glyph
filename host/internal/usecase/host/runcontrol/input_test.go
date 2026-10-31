//go:build !integration

package runcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestCoordinatorExecutesProcessedInput verifies Core receives the final input while the reservation is held.
func TestCoordinatorExecutesProcessedInput(t *testing.T) {
	t.Parallel()
	// Arrange input processing that returns a final message different from the submitted one.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	gate := NewMockGate(controller)
	sequence := make([]string, 0, 3)
	submitted := model.TextMessage("submitted")
	final := model.TextMessage("final")
	gate.EXPECT().TryAcquire().Return(func() { sequence = append(sequence, "release") }, true)
	gomock.InOrder(
		input.EXPECT().ProcessInput(gomock.Any(), submitted).DoAndReturn(
			func(context.Context, model.Message) (mo.Option[model.Message], error) {
				sequence = append(sequence, "input")
				return mo.Some(final), nil
			},
		),
		executor.EXPECT().Run(gomock.Any(), Request{RunID: "prepared", Message: final}).DoAndReturn(
			func(context.Context, Request) (Result, error) {
				sequence = append(sequence, "Core")
				return Result{Outcome: agent.RunOutcomeCompleted, SettlementRequired: false}, nil
			},
		),
	)
	coordinator := newCoordinator(executor, input, nil, func() (string, error) { return "prepared", nil }, gate)
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)

	// Act by executing the prepared run with the submitted message.
	outcome, err := coordinator.RunPrepared(t.Context(), runID, submitted)

	// Assert input processing precedes Core and the reservation is released last.
	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, outcome)
	assert.Equal(t, []string{"input", "Core", "release"}, sequence)
}

// TestCoordinatorFailsBeforeCoreWhenInputFails verifies an input failure skips Core and releases the reservation.
func TestCoordinatorFailsBeforeCoreWhenInputFails(t *testing.T) {
	t.Parallel()
	// Arrange input processing that rejects the submitted message and no Core expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	gate := NewMockGate(controller)
	released := 0
	gate.EXPECT().TryAcquire().Return(func() { released++ }, true)
	cause := errors.New("model text-only does not accept image input at content 0")
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), cause)
	coordinator := NewCoordinator(executor, input, nil, gate)

	// Act through the one-shot run entry point.
	outcome, err := coordinator.Run(t.Context(), model.TextMessage("request"))

	// Assert the failure keeps its complete cause and releases the reservation once.
	assert.Equal(t, agent.RunOutcomeFailed, outcome)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
	assert.Equal(t, 1, released)
}

// TestCoordinatorCompletesHandledInputWithoutCore verifies fully handled input skips Core and its settlement.
func TestCoordinatorCompletesHandledInputWithoutCore(t *testing.T) {
	t.Parallel()
	// Arrange handled input processing without Core or settlement expectations.
	controller := gomock.NewController(t)
	input := NewMockInputProcessor(controller)
	gate := NewMockGate(controller)
	released := 0
	gate.EXPECT().TryAcquire().Return(func() { released++ }, true)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), nil)
	coordinator := NewCoordinator(NewMockExecutor(controller), input, NewMockSettledDelivery(controller), gate)

	// Act through the one-shot run entry point.
	outcome, err := coordinator.Run(t.Context(), model.TextMessage("request"))

	// Assert successful completion and one reservation release.
	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, outcome)
	assert.Equal(t, 1, released)
}

// TestCoordinatorAbortsWhenInputIsCanceled verifies owner cancellation during input processing aborts the run.
func TestCoordinatorAbortsWhenInputIsCanceled(t *testing.T) {
	t.Parallel()
	// Arrange input processing that observes owner cancellation.
	controller := gomock.NewController(t)
	input := NewMockInputProcessor(controller)
	gate := NewMockGate(controller)
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), context.Canceled)
	coordinator := NewCoordinator(NewMockExecutor(controller), input, nil, gate)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through the one-shot run entry point.
	outcome, err := coordinator.Run(ctx, model.TextMessage("request"))

	// Assert the aborted outcome keeps the cancellation cause.
	assert.Equal(t, agent.RunOutcomeAborted, outcome)
	require.ErrorIs(t, err, context.Canceled)
}

// TestCoordinatorKeepsInputFailureDuringCancellation verifies an independent failure is not reported as aborted.
func TestCoordinatorKeepsInputFailureDuringCancellation(t *testing.T) {
	t.Parallel()
	// Arrange an extension failure acquired while the owner context is canceled.
	controller := gomock.NewController(t)
	input := NewMockInputProcessor(controller)
	gate := NewMockGate(controller)
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	cause := errors.New("middleware handler \"input\": handler failed")
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(
		mo.None[model.Message](), errors.Join(cause, context.Canceled),
	)
	coordinator := NewCoordinator(NewMockExecutor(controller), input, nil, gate)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through the one-shot run entry point.
	outcome, err := coordinator.Run(ctx, model.TextMessage("request"))

	// Assert the failed outcome keeps the independent cause.
	assert.Equal(t, agent.RunOutcomeFailed, outcome)
	require.ErrorIs(t, err, cause)
}

// newPassThroughInput returns input processing that keeps every submitted message.
func newPassThroughInput(controller *gomock.Controller) *MockInputProcessor {
	input := NewMockInputProcessor(controller)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, message model.Message) (mo.Option[model.Message], error) {
			return mo.Some(message), nil
		},
	).AnyTimes()
	return input
}
