//go:build integration

package project

import (
	"os"
	"syscall"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestServiceReadFileRejectsNamedPipe verifies project reads reject nonregular files without blocking.
func TestServiceReadFileRejectsNamedPipe(t *testing.T) {
	t.Parallel()

	// Arrange: create a FIFO and keep both ends open with enough buffered data for the old reader path.
	fifoPath := t.TempDir() + "/project-pipe"
	require.NoError(t, syscall.Mkfifo(fifoPath, 0o600))
	fifo, err := os.OpenFile(fifoPath, os.O_RDWR|syscall.O_NONBLOCK, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fifo.Close()) })
	_, err = fifo.Write(make([]byte, contentTypeHeaderSize))
	require.NoError(t, err)

	// Act: read the nonregular path through the production project service.
	_, err = New().ReadFile(t.Context(), fifoPath, mo.None[uint](), mo.None[uint]())

	// Assert: reject before opening or reading the FIFO and preserve path and file mode context.
	require.Error(t, err)
	require.ErrorContains(t, err, "not a regular project file")
	require.ErrorContains(t, err, fifoPath)
	require.ErrorContains(t, err, os.ModeNamedPipe.String())
}
