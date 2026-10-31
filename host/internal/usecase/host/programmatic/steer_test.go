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

// runSteerCommand prepares and runs one steer command against the coordinator in the given context.
func runSteerCommand(
	t *testing.T,
	ctx context.Context,
	coordinator Coordinator,
) operation.Outcome[controller.Response] {
	t.Helper()
	service := New(coordinator, nil, testStateQuery(t, true), nil, nil, nil, testRunOutput(t), nil, nil)
	command := testProgrammaticUserCommand("steer", "steer text")
	command.Kind = controller.CommandSteer
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	return prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
}

// TestSteerCommandReportsHandledMessage checks that a fully handled message completes with the handled result.
func TestSteerCommandReportsHandledMessage(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator whose input handlers fully handle the message.
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Steer(gomock.Any(), gomock.Any()).Return(true, nil)

	// Act through the prepared steer operation.
	outcome := runSteerCommand(t, t.Context(), coordinator)

	// Assert the operation completed with the handled result.
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
	response, present := outcome.Result()
	require.True(t, present)
	require.Equal(t, controller.ResponseEnqueueMessage, response.Kind)
	require.Equal(t, mo.Some(true), response.MessageHandled)
}

// TestSteerCommandFailsWithoutActiveRun checks the failure category of a message without an active agent run.
func TestSteerCommandFailsWithoutActiveRun(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator that reports no active run.
	cause := fmt.Errorf("queue steer message: %w", agent.ErrRunNotActive)
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Steer(gomock.Any(), gomock.Any()).Return(false, cause)

	// Act through the prepared steer operation.
	outcome := runSteerCommand(t, t.Context(), coordinator)

	// Assert the operation failed with its category and complete cause.
	require.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.Equal(t, controller.FailureCodeRunNotActive, outcome.Code())
	require.ErrorIs(t, outcome.Err(), agent.ErrRunNotActive)
}

// TestSteerCommandCanceled checks that cancellation of the operation during input processing cancels it.
func TestSteerCommandCanceled(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator that observes the cancellation of the operation context.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().Steer(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ model.Message) (bool, error) {
			cancel()
			return false, fmt.Errorf("process user input: %w", ctx.Err())
		},
	)

	// Act through the prepared steer operation.
	outcome := runSteerCommand(t, ctx, coordinator)

	// Assert the operation was canceled.
	require.Equal(t, operation.TerminalStateCanceled, outcome.State())
}
