//go:build !integration

package ui

import (
	"testing"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestSessionInformationOperationReturnsCoherentStatistics verifies retained information and statistics delivery.
func TestSessionInformationOperationReturnsCoherentStatistics(t *testing.T) {
	t.Parallel()
	// Arrange ActiveSessions to return one coherent information and statistics snapshot.
	controller := gomock.NewController(t)
	control := NewMockActiveSessions(controller)
	gate := NewMockGate(controller)
	control.EXPECT().ActiveInformation().Return(session.Info{}, session.Statistics{})
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, nil, gate, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)

	// Act by running the prepared GetSessionInfo operation.
	frame, err := runPreparedCommand(t, service, newCommandForPreparedTest(controllerui.CommandGetSessionInfo))

	// Assert one completed frame carries both information and statistics.
	require.NoError(t, err)
	assert.Equal(t, controllerui.FrameSessionInformation, frame.Kind)
	assert.True(t, frame.SessionInfo.IsSome())
	assert.True(t, frame.SessionStatistics.IsSome())
}
