//go:build integration

package app

import (
	"net/http"
	"sync/atomic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
)

// TestProgrammaticBlockedRunAllowsQueryAndTargetedCancellation verifies the real stream operation lifecycle.
func (testSuite *ProgrammaticAppSuite) TestProgrammaticBlockedRunAllowsQueryAndTargetedCancellation() {
	t := testSuite.T()

	// Arrange a provider request that remains blocked until its operation context is canceled.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{requestCount: requestCount, started: providerStarted}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildToolsExecutable(t))
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest("run", "first request")))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "run" && response.GetEvent().HasRunning() {
			break
		}
	}
	<-providerStarted

	// Act by querying the blocked run and then canceling it by operation identifier.
	state := completeProgrammaticRequest(t, fixture, runStateRequest("state")).GetRunState()
	assert.Equal(t, "run", state.GetActiveOperationId())
	require.NoError(t, fixture.stream.Send(cancelRequest("cancel", "run")))
	order := make([]string, 0, 2)
	var targetState operationv1.TerminalState
	for len(order) < 2 {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "run" && response.GetEvent().HasCanceled() {
			order = append(order, "run")
		}
		if response.GetOperationId() == "cancel" && response.GetEvent().HasCompleted() {
			order = append(order, "cancel")
			targetState = response.GetEvent().GetCompleted().GetCancel().GetTargetState()
		}
	}

	// Assert the target terminal event precedes cancellation completion.
	assert.Equal(t, []string{"run", "cancel"}, order)
	assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED, targetState)
}
