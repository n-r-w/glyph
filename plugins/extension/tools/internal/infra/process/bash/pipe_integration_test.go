//go:build integration

package bash

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestOutputPipePreservesReadAndCloseErrors retains both errors when a pipe fails before copying.
func TestOutputPipePreservesReadAndCloseErrors(t *testing.T) {
	t.Parallel()
	// Arrange a real pipe whose reader is already closed, with the production output destination.
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	sink := &outputSink{
		mutex: sync.Mutex{}, output: newOutputStore(),
		handleProgress: func(bashusecase.Stream, string) error { return nil }, cancel: cancel, progressErr: nil,
	}
	destination := &streamWriter{sink: sink, stream: bashusecase.StreamStdout, pending: nil}
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	defer func() { require.NoError(t, writer.Close()) }()
	require.NoError(t, reader.Close())
	pipe := &outputPipe{reader: reader, writer: writer, destination: destination}
	result := make(chan error, 1)
	// Act through the real copy and close path.
	pipe.copy(cancel, result)
	completionErr := <-result
	// Assert cancellation starts and the independent copy and close errors remain visible.
	require.ErrorIs(t, completionErr, os.ErrClosed)
	assert.Contains(t, completionErr.Error(), "copy bash output:")
	assert.Contains(t, completionErr.Error(), "close bash output pipe:")
	require.ErrorIs(t, context.Cause(ctx), os.ErrClosed)
}

// TestProcessOutputClosesAllEndpoints releases both read and write sides on a start failure cleanup path.
func TestProcessOutputClosesAllEndpoints(t *testing.T) {
	t.Parallel()
	// Arrange the production pipe allocation without starting a process.
	output, err := newProcessOutput(nil, nil)
	require.NoError(t, err)
	// Act through start failure cleanup.
	require.NoError(t, output.close())
	// Assert that every owned descriptor is closed, rather than only its child-facing writer.
	for _, file := range []*os.File{
		output.stdout.reader, output.stdout.writer, output.stderr.reader, output.stderr.writer,
	} {
		_, statErr := file.Stat()
		require.ErrorIs(t, statErr, os.ErrClosed)
	}
}
