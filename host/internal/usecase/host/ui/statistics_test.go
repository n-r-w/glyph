//go:build !integration

package ui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSessionInformationOperationReturnsCoherentStatistics verifies retained information and statistics delivery.
func TestSessionInformationOperationReturnsCoherentStatistics(t *testing.T) {
	t.Parallel()
	// Arrange SessionControl to return one coherent information and statistics snapshot.
	controller := gomock.NewController(t)
	control := NewMockSessionControl(controller)
	snapshot := session.InformationSnapshot{Info: session.Info{}, Statistics: session.Statistics{}}
	control.EXPECT().Information().Return(snapshot)
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)

	// Act by running the prepared GetSessionInfo operation.
	frame, err := runPreparedCommand(t, service, newCommandForPreparedTest(domainui.CommandGetSessionInfo))

	// Assert one completed frame carries both information and statistics.
	require.NoError(t, err)
	assert.Equal(t, domainui.FrameSessionInformation, frame.Kind)
	assert.True(t, frame.SessionInfo.IsSome())
	assert.True(t, frame.SessionStatistics.IsSome())
}
