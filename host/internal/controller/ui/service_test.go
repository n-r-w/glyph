//go:build !integration

package ui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestInitializationFailureStopsCommandProcessing checks the controller preserves startup failure before activation.
func TestInitializationFailureStopsCommandProcessing(t *testing.T) {
	t.Parallel()
	// Arrange the selected stream and a Host initialization failure with its original cause.
	mocks := gomock.NewController(t)
	source := NewMockStreamSource(mocks)
	connection := NewMockConnection(mocks)
	session := NewMockSession(mocks)
	cause := errors.New("UI process exited during initialization")
	failure := fmt.Errorf("send UI initialization: %w", cause)
	source.EXPECT().Open(t.Context()).Return(connection, nil)
	session.EXPECT().Initialize(t.Context()).Return(failure)
	controller := New(source)
	require.NoError(t, controller.Open(t.Context()))

	// Act through controller startup. The mocks reject activation, writer attachment, or command receipt.
	err := controller.Execute(t.Context(), session)

	// Assert the complete initialization cause reaches the caller with controller context.
	require.ErrorIs(t, err, cause)
	require.EqualError(t, err, "execute UI session: "+failure.Error())
}
