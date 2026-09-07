//go:build !integration

package terminal

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestRuntimeOpenStartsFreshFrame prevents inherited connection failure from changing the next program's lifetime.
func TestRuntimeOpenStartsFreshFrame(t *testing.T) {
	t.Parallel()
	// Arrange a finished runtime whose prior frame records source termination and dimensions.
	controller := gomock.NewController(t)
	device := NewMockDevice(controller)
	files := NewMockFiles(controller)
	device.EXPECT().Open().Times(2).Return(files, nil)
	files.EXPECT().Close().Times(2).Return(nil)
	runtime := NewRuntime(device)
	require.NoError(t, runtime.Open())
	runtime.model.failure = errors.New("previous source failure")
	runtime.model.notificationEnded = true
	runtime.model.width, runtime.model.height = 80, 24
	require.NoError(t, runtime.Close())

	// Act by opening the resources for a later connection.
	require.NoError(t, runtime.Open())

	// Assert the new program starts without inherited shutdown or viewport state.
	require.NoError(t, runtime.model.failure)
	require.False(t, runtime.model.notificationEnded)
	require.Zero(t, runtime.model.width)
	require.Zero(t, runtime.model.height)
	require.NoError(t, runtime.Close())
}
