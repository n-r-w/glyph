//go:build !integration

package plugin

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestUnconsumedInitializationClosesPreparedTerminal verifies failed startup cleanup ownership.
func TestUnconsumedInitializationClosesPreparedTerminal(t *testing.T) {
	t.Parallel()

	// Arrange successful terminal preparation that Service.Run does not consume.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	terminal.EXPECT().Open().Return(session, nil)
	closeCause := errors.New("restore terminal failed")
	session.EXPECT().Close().Return(closeCause)
	controller := New(terminal, NewMockProgramFactory(mockController))
	prepared, err := controller.PrepareInitialize(t.Context(), validInitialization())
	require.NoError(t, err)
	initialized, err := prepared.Run(t.Context())
	require.NoError(t, err)
	require.NotNil(t, initialized)
	prepared.Release()

	// Act by cleaning resources after initialization delivery did not start Service.Run.
	err = controller.Close()

	// Assert the complete terminal close cause remains reachable.
	require.Error(t, err)
	assert.ErrorIs(t, err, closeCause)
	assert.ErrorContains(t, err, closeCause.Error())
}

// TestInitializationTerminalOpenFailureFailsOperation verifies startup effects precede completion.
func TestInitializationTerminalOpenFailureFailsOperation(t *testing.T) {
	t.Parallel()

	// Arrange initialization whose required terminal cannot open.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	source := errors.New("controlling terminal unavailable")
	terminal.EXPECT().Open().Return(nil, source)
	controller := New(terminal, NewMockProgramFactory(mockController))
	prepared, err := controller.PrepareInitialize(t.Context(), validInitialization())
	require.NoError(t, err)

	// Act through initialization work.
	initialized, err := prepared.Run(t.Context())
	prepared.Release()

	// Assert initialization fails with the terminal source cause.
	require.Error(t, err)
	assert.ErrorIs(t, err, source)
	assert.ErrorContains(t, err, source.Error())
	assert.Nil(t, initialized)
}
