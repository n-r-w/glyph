//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSetQueueModeCommandCompletesWithSelectedMode checks that the command passes the mode to the coordinator and
// completes with it, while an agent run is active and without a session-mutation reservation.
func TestSetQueueModeCommandCompletesWithSelectedMode(t *testing.T) {
	t.Parallel()
	// Arrange a coordinator that expects the mode, and a service without a gate, which would fail on a reservation.
	coordinator := NewMockCoordinator(gomock.NewController(t))
	coordinator.EXPECT().SetQueueMode(agent.QueueModeAll)
	service := New(coordinator, nil, testStateQuery(t, true), nil, nil, nil, testRunOutput(t), nil, nil)
	command := testProgrammaticUserCommand("queue-mode", "")
	command.Kind = controller.CommandSetQueueMode
	command.UserMessage = mo.None[model.Message]()
	command.QueueMode = mo.Some(agent.QueueModeAll)

	// Act by preparing and running the command.
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	outcome := prepared.Run(t.Context(), operation.Reporter[controller.OperationProgress]{})

	// Assert the operation completed with the selected mode.
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
	response, present := outcome.Result()
	require.True(t, present)
	require.Equal(t, controller.ResponseQueueMode, response.Kind)
	require.Equal(t, mo.Some(agent.QueueModeAll), response.QueueMode)
}
