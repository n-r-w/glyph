//go:build integration

package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardAutomaticCompactionCompletesRun checks terminal delivery after prepared history shrinks.
func TestStandardAutomaticCompactionCompletesRun(t *testing.T) {
	// Arrange ten completed turns through the bundled extension and production assembly.
	t.Setenv(standardRetainedEnvironment, "2000")
	settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 3500")
	settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	bodies := installStandardConversationTransport(t, finalResponseSSE)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildStandardCompaction(t))
	defer fixture.closeOwner(t)
	for index := range 10 {
		completeProgrammaticRequest(t, fixture, userRequest(fmt.Sprintf("prior-%d", index), strings.Repeat("o", 400)))
	}

	// Act through the public request that replaces the prepared prefix during the run.
	const operationID = "shortened-history"
	require.NoError(t, fixture.stream.Send(userRequest(operationID, strings.Repeat("r", 8000))))
	var summary *programmaticpb.AgentSummary
	var terminal *programmaticpb.HostEvent
	for terminal == nil {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != operationID {
			continue
		}
		event := response.GetEvent()
		if agentSummary := event.GetProgress().GetAgentEvent().GetAgent(); agentSummary != nil {
			summary = agentSummary
		}
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() || event.HasRejected() {
			terminal = event
		}
	}

	// Assert successful lifecycle delivery, durable replacement, shorter input and idle settlement.
	require.NotNil(t, terminal.GetCompleted())
	require.NotNil(t, summary)
	require.Equal(t, programmaticpb.RunOutcome_RUN_OUTCOME_COMPLETED, summary.GetOutcome())
	require.Empty(t, summary.GetErrorMessage())
	state := sendProgrammaticOperation(t, fixture, "settled-state", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetRunState(new(programmaticpb.GetRunState))
	}).GetRunState()
	require.Equal(t, programmaticpb.RunState_RUN_STATE_IDLE, state.GetState())
	require.Empty(t, state.GetActiveOperationId())
	stored := sendProgrammaticOperation(t, fixture, "stored-history", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	markers := 0
	for _, entry := range stored {
		if entry.GetCompaction() != nil {
			markers++
		}
	}
	require.Equal(t, 1, markers)
	requests := bodies.snapshot()
	require.Len(t, requests, 12)
	require.NotContains(t, string(requests[11]), strings.Repeat("o", 400))
	require.Contains(t, string(requests[11]), strings.Repeat("r", 8000))
}
