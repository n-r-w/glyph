package terminal

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

	inputReader, inputWriter, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = inputWriter.Close() })
	outputReader, outputWriter, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = outputReader.Close() })

	service := newWithOpen(func() (*os.File, *os.File, error) {
		return inputReader, outputWriter, nil
	})
	session, err := service.Open()
	require.NoError(t, err)
	assert.Same(t, inputReader, session.Input())
	assert.Same(t, outputWriter, session.Output())
	require.NoError(t, session.Close())
	require.ErrorIs(t, inputReader.Close(), os.ErrClosed)
	assert.ErrorIs(t, outputWriter.Close(), os.ErrClosed)
}

// TestSessionClosesSharedTTYFileOnce verifies aliased terminal files are not double-closed.
func TestSessionClosesSharedTTYFileOnce(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	service := newWithOpen(func() (*os.File, *os.File, error) {
		return reader, reader, nil
	})
	session, err := service.Open()
	require.NoError(t, err)
	require.NoError(t, session.Close())
	assert.ErrorIs(t, reader.Close(), os.ErrClosed)
}

// TestServiceReturnsOpenTTYFailure verifies terminal acquisition errors remain explicit.
func TestServiceReturnsOpenTTYFailure(t *testing.T) {
	t.Parallel()

	service := newWithOpen(func() (*os.File, *os.File, error) {
		return nil, nil, errors.New("no controlling terminal")
	})
	_, err := service.Open()
	require.EqualError(t, err, "open controlling terminal: no controlling terminal")
}
