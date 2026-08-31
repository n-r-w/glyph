//go:build !integration

package bash

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestStreamWriterJoinsSplitUTF8 keeps protobuf progress valid across pipe fragments.
func TestStreamWriterJoinsSplitUTF8(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(t.Context())
	fragments := make([]string, 0, 1)
	sink := &outputSink{
		mutex: sync.Mutex{}, output: newOutputStore(),
		handleProgress: func(_ bashusecase.Stream, content string) error {
			fragments = append(fragments, content)
			return nil
		},
		cancel: cancel,
	}
	writer := &streamWriter{sink: sink, stream: bashusecase.StreamStdout, pending: nil}

	_, err := writer.Write([]byte{0xe2})
	require.NoError(t, err)
	_, err = writer.Write([]byte{0x82, 0xac})
	require.NoError(t, err)

	assert.Equal(t, []string{"€"}, fragments)
}

// TestStreamWriterKeepsDeliveredTextOrder across interleaved stdout and stderr bytes.
func TestStreamWriterKeepsDeliveredTextOrder(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(t.Context())
	fragments := make([]string, 0, 2)
	sink := &outputSink{
		mutex: sync.Mutex{}, output: newOutputStore(),
		handleProgress: func(_ bashusecase.Stream, content string) error {
			fragments = append(fragments, content)
			return nil
		},
		cancel: cancel,
	}
	stdout := &streamWriter{sink: sink, stream: bashusecase.StreamStdout, pending: nil}
	stderr := &streamWriter{sink: sink, stream: bashusecase.StreamStderr, pending: nil}

	_, err := stdout.Write([]byte{0xe2})
	require.NoError(t, err)
	_, err = stderr.Write([]byte("x"))
	require.NoError(t, err)
	_, err = stdout.Write([]byte{0x82, 0xac})
	require.NoError(t, err)
	result, err := sink.result(0, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"x", "€"}, fragments)
	assert.Equal(t, "x€\n\n[Exit code: 0]\n", result.Output)
}

// TestStreamWriterReplacesInvalidUTF8 keeps streamed and terminal text valid and explicit.
func TestStreamWriterReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(t.Context())
	fragments := make([]string, 0, 1)
	sink := &outputSink{
		mutex: sync.Mutex{}, output: newOutputStore(),
		handleProgress: func(_ bashusecase.Stream, content string) error {
			fragments = append(fragments, content)
			return nil
		},
		cancel: cancel,
	}
	writer := &streamWriter{sink: sink, stream: bashusecase.StreamStdout, pending: nil}

	_, err := writer.Write([]byte{0xff, 'a'})
	require.NoError(t, err)
	result, err := sink.result(0, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"?a"}, fragments)
	assert.Equal(t, "?a\n\n[Exit code: 0]\n", result.Output)
}
