//go:build !integration

package ui

import (
	"context"
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
	// Arrange SessionControl to return one coherent information and statistics snapshot.
	controller := gomock.NewController(t)
	control := NewMockSessionControl(controller)
	gate := NewMockGate(controller)
	snapshot := session.InformationSnapshot{Info: session.Info{}, Statistics: session.Statistics{}}
	control.EXPECT().Information().Return(snapshot)
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, gate, func(context.Context) {},

		Initialization{},
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
