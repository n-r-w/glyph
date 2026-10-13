//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// manualFollowupOwnerUnavailable identifies scenarios that deliberately shut down the registered owner.
func manualFollowupOwnerUnavailable(guidance string) bool {
	return guidance == manualFixtureLoseStateAfterCommit || guidance == manualFixtureLoseSharedOwnerAfterCommit
}

// assertManualUnavailableOwnerFollowup checks failed conversation admission and retained durable replacement state.
func assertManualUnavailableOwnerFollowup(t *testing.T, fixture *programmaticFixture, bodies *requestBodies) {
	t.Helper()
	before := len(bodies.snapshot())
	request := userRequest(manualFixtureNext, manualNextText)
	require.NoError(t, fixture.stream.Send(request))
	var terminal *programmaticpb.HostEvent
	for terminal == nil {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != request.GetOperationId() {
			continue
		}
		event := response.GetEvent()
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() || event.HasRejected() {
			terminal = event
		}
	}
	require.NotNil(t, terminal.GetFailed())
	require.Equal(t, conversationExtensionFailed, terminal.GetFailed().GetCode())
	require.Contains(t, terminal.GetFailed().GetMessage(), "issue captured conversation policy context")
	require.Contains(t, terminal.GetFailed().GetMessage(), "unavailable")
	require.Len(t, bodies.snapshot(), before)
	state := sendProgrammaticOperation(
		t,
		fixture,
		"unavailable-owner-followup-state",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetRunState(new(programmaticpb.GetRunState))
		},
	).GetRunState()
	require.Equal(t, programmaticpb.RunState_RUN_STATE_IDLE, state.GetState())
	require.Empty(t, state.GetActiveOperationId())
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"unavailable-owner-followup-history",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	originals, markers := countManualStoredEntries(stored)
	require.Equal(t, 1, originals)
	require.Equal(t, 1, markers)
	last := stored[len(stored)-1].GetModel()
	require.NotNil(t, last)
	require.Equal(t, programmaticpb.ModelOutcome_MODEL_OUTCOME_FAILED, last.GetOutcome())
	require.Contains(t, last.GetErrorMessage(), "unavailable")
	nextUsers := 0
	for _, entry := range stored {
		for _, content := range entry.GetUser().GetContent() {
			if content.GetText() == manualNextText {
				nextUsers++
			}
		}
	}
	require.Equal(t, 1, nextUsers)
	require.Len(t, bodies.snapshot(), before)
}
