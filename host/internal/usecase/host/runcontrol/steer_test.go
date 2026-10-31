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

// TestCoordinatorSteerQueuesProcessedInput verifies that Core queues the final message of input processing for the
// run that the admission check returned.
func TestCoordinatorSteerQueuesProcessedInput(t *testing.T) {
	t.Parallel()
	// Arrange an active run and input processing that returns a final message different from the submitted one.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	submitted := model.TextMessage("submitted")
	final := model.TextMessage("final")
	gomock.InOrder(
		executor.EXPECT().CheckSteer().Return("run", nil),
		input.EXPECT().ProcessInput(gomock.Any(), submitted).Return(mo.Some(final), nil),
		executor.EXPECT().Steer("run", final).Return(nil),
	)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing the submitted message.
	handled, err := coordinator.Steer(t.Context(), submitted)

	// Assert Core received the final message and the result reports a queued message.
	require.NoError(t, err)
	require.False(t, handled)
}

// TestCoordinatorSteerSkipsHandledInput verifies that a fully handled message does not reach Core.
func TestCoordinatorSteerSkipsHandledInput(t *testing.T) {
	t.Parallel()
	// Arrange an active run, input processing that fully handles the message, and no queue expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	executor.EXPECT().CheckSteer().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), nil)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Steer(t.Context(), model.TextMessage("handled"))

	// Assert the result reports the handled message.
	require.NoError(t, err)
	require.True(t, handled)
}

// TestCoordinatorSteerRejectsBeforeInputProcessing verifies that input processing does not run without an active run.
func TestCoordinatorSteerRejectsBeforeInputProcessing(t *testing.T) {
	t.Parallel()
	// Arrange Core without an active run and no input-processing expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	executor.EXPECT().CheckSteer().Return("", agent.ErrRunNotActive)
	coordinator := NewCoordinator(executor, NewMockInputProcessor(controller), nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Steer(t.Context(), model.TextMessage("steer"))

	// Assert the command fails with the Core cause.
	require.ErrorIs(t, err, agent.ErrRunNotActive)
	require.False(t, handled)
}

// TestCoordinatorSteerFailsWhenInputFails verifies that an input failure does not reach Core.
func TestCoordinatorSteerFailsWhenInputFails(t *testing.T) {
	t.Parallel()
	// Arrange an active run, failing input processing, and no queue expectation.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	cause := errors.New("reference input handler failed")
	executor.EXPECT().CheckSteer().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.None[model.Message](), cause)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Steer(t.Context(), model.TextMessage("steer"))

	// Assert the command fails with the complete input cause.
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
	require.False(t, handled)
}

// TestCoordinatorSteerFailsWhenQueueRejects verifies that a run that ended during input processing fails the command.
func TestCoordinatorSteerFailsWhenQueueRejects(t *testing.T) {
	t.Parallel()
	// Arrange an active run that ends before Core receives the processed message.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	input := NewMockInputProcessor(controller)
	final := model.TextMessage("final")
	executor.EXPECT().CheckSteer().Return("run", nil)
	input.EXPECT().ProcessInput(gomock.Any(), gomock.Any()).Return(mo.Some(final), nil)
	executor.EXPECT().Steer("run", final).Return(agent.ErrRunNotActive)
	coordinator := NewCoordinator(executor, input, nil, nil)

	// Act by queuing a message.
	handled, err := coordinator.Steer(t.Context(), model.TextMessage("steer"))

	// Assert the command fails with the Core cause.
	require.ErrorIs(t, err, agent.ErrRunNotActive)
	require.False(t, handled)
}
