//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// requestProgrammaticFailure retains the failed public terminal and counts only its run-scoped retry progress.
func requestProgrammaticFailure(
	t *testing.T,
	fixture *programmaticFixture,
	request *programmaticpb.OpenRequest,
) (terminalFailureObservation, int) {
	t.Helper()
	require.NoError(t, fixture.stream.Send(request))
	retryProgress := 0
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != request.GetOperationId() {
			continue
		}
		event := response.GetEvent()
		if event.GetProgress().GetAgentEvent().GetRetryProgress() != nil {
			retryProgress++
		}
		if failure := event.GetFailed(); failure != nil {
			return terminalFailureObservation{
				Code:    failure.GetFailure().GetCode(),
				Message: failure.GetFailure().GetMessage(),
			}, retryProgress
		}
		require.False(t, event.HasCompleted() || event.HasCanceled(), "unexpected successful or canceled terminal")
	}
}
