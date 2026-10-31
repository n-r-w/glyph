//go:build !integration

package runcontrol

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestCoordinatorEnqueueQueuesProcessedInput verifies that Core queues, with its delivery kind, the final message
// of input processing for the run that the admission check returned.
func TestCoordinatorEnqueueQueuesProcessedInput(t *testing.T) {
	t.Parallel()
	// Arrange an active run and input processing that returns a final message different from the submitted one.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	submitted := model.TextMessage("submitted")
	final := model.TextMessage("final")
	gomock.InOrder(
		executor.EXPECT().CheckEnqueue().Return("run", nil),
		input.EXPECT().ProcessInput(gomock.Any(), submitted).Return(mo.Some(final), nil),
		executor.EXPECT().Enqueue("run", agent.MessageDeliverySteer, final).Return(nil),
	)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing the submitted message.
	handled, err := coordinator.Enqueue(t.Context(), agent.MessageDeliverySteer, submitted)

	// Assert Core received the final message and the result reports a queued message.
	require.NoError(t, err)
	require.False(t, handled)
}

// TestCoordinatorEnqueueSkipsHandledInput verifies that a fully handled message does not reach Core.
func TestCoordinatorEnqueueSkipsHandledInput(t *testing.T) {
	t.Parallel()
	// Arrange an active run, input processing that fully handles the message, and no queue expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	executor.EXPECT().CheckEnqueue().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), nil)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Enqueue(t.Context(), agent.MessageDeliverySteer, model.TextMessage("handled"))

	// Assert the result reports the handled message.
	require.NoError(t, err)
	require.True(t, handled)
}

// TestCoordinatorEnqueueRejectsBeforeInputProcessing verifies that input processing does not run without an active run.
func TestCoordinatorEnqueueRejectsBeforeInputProcessing(t *testing.T) {
	t.Parallel()
	// Arrange Core without an active run and no input-processing expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	executor.EXPECT().CheckEnqueue().Return("", agent.ErrRunNotActive)
	coordinator := NewCoordinator(executor, NewMockInputProcessor(controller), nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Enqueue(t.Context(), agent.MessageDeliverySteer, model.TextMessage("steer"))

	// Assert the command fails with the Core cause.
	require.ErrorIs(t, err, agent.ErrRunNotActive)
	require.False(t, handled)
}

// TestCoordinatorEnqueueFailsWhenInputFails verifies that an input failure does not reach Core.
func TestCoordinatorEnqueueFailsWhenInputFails(t *testing.T) {
	t.Parallel()
	// Arrange an active run, failing input processing, and no queue expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	cause := errors.New("reference input handler failed")
	executor.EXPECT().CheckEnqueue().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), cause)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Enqueue(t.Context(), agent.MessageDeliverySteer, model.TextMessage("steer"))

	// Assert the command fails with the complete input cause.
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
	require.False(t, handled)
}

// TestCoordinatorEnqueueFailsWhenQueueRejects verifies that a run that ended during input processing fails the command.
func TestCoordinatorEnqueueFailsWhenQueueRejects(t *testing.T) {
	t.Parallel()
	// Arrange an active run that ends before Core receives the processed message.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	final := model.TextMessage("final")
	executor.EXPECT().CheckEnqueue().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.Some(final), nil)
	executor.EXPECT().Enqueue("run", agent.MessageDeliverySteer, final).Return(agent.ErrRunNotActive)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Enqueue(t.Context(), agent.MessageDeliverySteer, model.TextMessage("steer"))

	// Assert the command fails with the Core cause.
	require.ErrorIs(t, err, agent.ErrRunNotActive)
	require.False(t, handled)
}

// TestCoordinatorEnqueuePassesFollowUpDelivery verifies that the followUp delivery kind reaches Core unchanged.
func TestCoordinatorEnqueuePassesFollowUpDelivery(t *testing.T) {
	t.Parallel()
	// Arrange an active run and input processing that keeps the message.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	message := model.TextMessage("queued")
	executor.EXPECT().CheckEnqueue().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), message).Return(mo.Some(message), nil)
	executor.EXPECT().Enqueue("run", agent.MessageDeliveryFollowUp, message).Return(nil)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing the message as followUp.
	handled, err := coordinator.Enqueue(t.Context(), agent.MessageDeliveryFollowUp, message)

	// Assert Core received the kind and the result reports a queued message.
	require.NoError(t, err)
	require.False(t, handled)
}
