//go:build integration

package device

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceOpensAndSessionClosesBothTTYFiles verifies distinct terminal files close once.
func TestServiceOpensAndSessionClosesBothTTYFiles(t *testing.T) {
	t.Parallel()

	// Arrange independent input and output pipes.
	inputReader, inputWriter, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = inputWriter.Close() })
	outputReader, outputWriter, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = outputReader.Close() })

	service := newWithOpen(func() (*os.File, *os.File, error) {
		return inputReader, outputWriter, nil
	})
	// Act by opening the terminal session.
	session, err := service.Open()
	require.NoError(t, err)
	// Assert file identity and exactly-once closure.
	assert.Same(t, inputReader, session.Input())
	assert.Same(t, outputWriter, session.Output())
	require.NoError(t, session.Close())
	require.ErrorIs(t, inputReader.Close(), os.ErrClosed)
	require.ErrorIs(t, outputWriter.Close(), os.ErrClosed)
}

// TestSessionClosesSharedTTYFileOnce verifies aliased terminal files are not double-closed.
func TestSessionClosesSharedTTYFileOnce(t *testing.T) {
	t.Parallel()

	// Arrange one file shared by both terminal directions.
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	service := newWithOpen(func() (*os.File, *os.File, error) {
		return reader, reader, nil
	})
	// Act by opening the shared-file session.
	session, err := service.Open()
	require.NoError(t, err)
	// Assert cleanup closes the shared file once.
	require.NoError(t, session.Close())
	require.ErrorIs(t, reader.Close(), os.ErrClosed)
}

// TestServiceReturnsOpenTTYFailure verifies terminal acquisition errors remain explicit.
func TestServiceReturnsOpenTTYFailure(t *testing.T) {
	t.Parallel()

	// Arrange a failed controlling-terminal acquisition.
	service := newWithOpen(func() (*os.File, *os.File, error) {
		return nil, nil, errors.New("no controlling terminal")
	})
	// Act by opening the device.
	_, err := service.Open()
	// Assert the complete acquisition cause remains visible.
	require.EqualError(t, err, "open controlling terminal: no controlling terminal")
}
