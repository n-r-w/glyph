//go:build !integration

package programmatic

import (
	"context"
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/operation"
)

// runEnqueueCommand prepares and runs one enqueue command against the coordinator in the given context.
func runEnqueueCommand(
	t *testing.T,
	ctx context.Context,
	coordinator Coordinator,
	delivery agent.MessageDelivery,
) operation.Outcome[controller.Response] {
	t.Helper()
	service := New(coordinator, nil, testStateQuery(t, true), nil, nil, nil, testRunOutput(t), nil, nil)
	command := testProgrammaticUserCommand("enqueue", "queued text")
	command.Kind = controller.CommandEnqueueMessage
	command.Delivery = mo.Some(delivery)
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	return prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
}

// TestEnqueueCommandReportsHandledMessage checks that a fully handled message completes with the handled result.
func TestEnqueueCommandReportsHandledMessage(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator whose input handlers fully handle the message.
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Enqueue(gomock.Any(), agent.MessageDeliverySteer, gomock.Any()).Return(true, nil)

	// Act through the prepared enqueue operation.
	outcome := runEnqueueCommand(t, t.Context(), coordinator, agent.MessageDeliverySteer)

	// Assert the operation completed with the handled result.
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
	response, present := outcome.Result()
	require.True(t, present)
	require.Equal(t, controller.ResponseEnqueueMessage, response.Kind)
	require.Equal(t, mo.Some(true), response.MessageHandled)
}

// TestEnqueueCommandFailsWithoutActiveRun checks the failure category of a message without an active agent run.
func TestEnqueueCommandFailsWithoutActiveRun(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator that reports no active run.
	cause := fmt.Errorf("queue message: %w", agent.ErrRunNotActive)
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Enqueue(gomock.Any(), agent.MessageDeliverySteer, gomock.Any()).Return(false, cause)

	// Act through the prepared enqueue operation.
	outcome := runEnqueueCommand(t, t.Context(), coordinator, agent.MessageDeliverySteer)

	// Assert the operation failed with its category and complete cause.
	require.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.Equal(t, controller.FailureCodeRunNotActive, outcome.Code())
	require.ErrorIs(t, outcome.Err(), agent.ErrRunNotActive)
}

// TestEnqueueCommandCanceled checks that cancellation of the operation during input processing cancels it.
func TestEnqueueCommandCanceled(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator that observes the cancellation of the operation context.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Enqueue(gomock.Any(), agent.MessageDeliverySteer, gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ agent.MessageDelivery, _ model.Message) (bool, error) {
			cancel()
			return false, fmt.Errorf("process user input: %w", ctx.Err())
		},
	)

	// Act through the prepared enqueue operation.
	outcome := runEnqueueCommand(t, ctx, coordinator, agent.MessageDeliverySteer)

	// Assert the operation was canceled.
	require.Equal(t, operation.TerminalStateCanceled, outcome.State())
}

// TestEnqueueCommandPassesFollowUpDelivery checks that the delivery kind of the command reaches the coordinator.
func TestEnqueueCommandPassesFollowUpDelivery(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator that expects the followUp delivery kind.
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Enqueue(gomock.Any(), agent.MessageDeliveryFollowUp, gomock.Any()).Return(false, nil)

	// Act through the prepared enqueue operation.
	outcome := runEnqueueCommand(t, t.Context(), coordinator, agent.MessageDeliveryFollowUp)

	// Assert the operation completed with a queued message.
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
	response, present := outcome.Result()
	require.True(t, present)
	require.Equal(t, mo.Some(false), response.MessageHandled)
}

// TestEnqueueCommandFailsWithoutDelivery checks that a prepared enqueue command without a delivery kind fails as an
// internal error and never reaches the coordinator.
func TestEnqueueCommandFailsWithoutDelivery(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator without expectations and a prepared command that lacks the delivery kind.
	coordinator := NewMockCoordinator(gomock.NewController(t))
	service := New(coordinator, nil, testStateQuery(t, true), nil, nil, nil, testRunOutput(t), nil, nil)
	command := testProgrammaticUserCommand("enqueue", "queued text")
	command.Kind = controller.CommandEnqueueMessage
	prepared := &commandPrepared{service: service, command: command, release: func() {}, selection: nil}

	// Act through the prepared enqueue operation.
	outcome := prepared.Run(t.Context(), operation.Reporter[controller.OperationProgress]{})

	// Assert the operation failed with the internal category and a complete cause.
	require.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.Equal(t, controller.FailureCodeInternal, outcome.Code())
	require.ErrorContains(t, outcome.Err(), "delivery kind")
}
