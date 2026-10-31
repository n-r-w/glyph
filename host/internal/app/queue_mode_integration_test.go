//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// sendQueueMode selects a queue mode and returns the terminal lifecycle event of its operation.
func sendQueueMode(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
	mode programmaticv1.QueueMode,
) *programmaticv1.HostEvent {
	t.Helper()
	require.NoError(t, fixture.stream.Send(testProgrammaticRequest(
		operationID, func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.SetQueueMode)
			payload.SetMode(mode)
			request.SetSetQueueMode(payload)
		},
	)))
	return waitForTerminalEvent(t, fixture, operationID)
}

// requireQueueMode checks that the queue mode selection completed with the selected mode.
func requireQueueMode(t *testing.T, event *programmaticv1.HostEvent, mode programmaticv1.QueueMode) {
	t.Helper()
	require.True(t, event.HasCompleted(), "queue mode operation did not complete: %v", event)
	require.True(t, event.GetCompleted().HasQueueMode())
	require.Equal(t, mode, event.GetCompleted().GetQueueMode().GetMode())
}

// TestPublicQueueModeReplacedByOneAtATime checks that selecting one-at-a-time after all replaces the setting, so each
// model request receives one steer message, and that the selection completes while the agent run holds the operation
// gate.
func TestPublicQueueModeReplacedByOneAtATime(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the final response of the first request, with an agent run in progress.
	provider := startQueueProvider(t, 1, finalResponseSSE, finalResponseSSE, finalResponseSSE)
	fixture := startQueueFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(queueRunID, "first request")))
	<-provider.held[0]

	// Act by selecting mode all and then one-at-a-time, queuing two steer messages, and releasing the final response.
	all := sendQueueMode(t, fixture, "mode-all", programmaticv1.QueueMode_QUEUE_MODE_ALL)
	selected := sendQueueMode(t, fixture, "mode-one", programmaticv1.QueueMode_QUEUE_MODE_ONE_AT_A_TIME)
	first := sendSteer(t, fixture, "steer-first", steerFirstText)
	second := sendSteer(t, fixture, "steer-second", steerSecondText)
	close(provider.release[0])
	terminal := waitForTerminalEvent(t, fixture, queueRunID)

	// Assert both selections completed with their modes, and each following model request adds one steer message.
	requireQueueMode(t, all, programmaticv1.QueueMode_QUEUE_MODE_ALL)
	requireQueueMode(t, selected, programmaticv1.QueueMode_QUEUE_MODE_ONE_AT_A_TIME)
	requireQueued(t, first)
	requireQueued(t, second)
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	bodies := provider.snapshot()
	require.Len(t, bodies, 3)
	require.Contains(t, bodies[1], steerFirstText)
	require.NotContains(t, bodies[1], steerSecondText)
	require.Contains(t, bodies[2], steerSecondText)
}

// TestPublicQueueModeAllSteer checks that the selected mode all gives the next model request every steer message
// that was queued while a request was held.
func TestPublicQueueModeAllSteer(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the final response of the first request, with an agent run in progress.
	provider := startQueueProvider(t, 1, finalResponseSSE, finalResponseSSE)
	fixture := startQueueFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(queueRunID, "first request")))
	<-provider.held[0]

	// Act by selecting mode all and queuing two steer messages, then releasing the final response.
	selected := sendQueueMode(t, fixture, "mode-all", programmaticv1.QueueMode_QUEUE_MODE_ALL)
	first := sendSteer(t, fixture, "steer-first", steerFirstText)
	second := sendSteer(t, fixture, "steer-second", steerSecondText)
	close(provider.release[0])
	terminal := waitForTerminalEvent(t, fixture, queueRunID)

	// Assert the selection completed with its mode, and the second model request contains both steer messages.
	requireQueueMode(t, selected, programmaticv1.QueueMode_QUEUE_MODE_ALL)
	requireQueued(t, first)
	requireQueued(t, second)
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	require.NotContains(t, bodies[0], steerFirstText)
	require.Contains(t, bodies[1], steerFirstText)
	require.Contains(t, bodies[1], steerSecondText)
}
