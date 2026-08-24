package bash

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// TestOutputStoreRemovesIncompleteFileAfterWriteFailure never advertises partial retained output.
func TestOutputStoreRemovesIncompleteFileAfterWriteFailure(t *testing.T) {
	t.Parallel()

	store := newOutputStore()
	require.NoError(t, store.append([]byte(strings.Repeat("x", textbudget.MaximumBytes+1))))
	path := store.path
	t.Cleanup(func() { _ = os.Remove(path) })
	require.NoError(t, store.file.Close())
	reader, writer := io.Pipe()
	writeErr := errors.New("output storage full")
	require.NoError(t, reader.CloseWithError(writeErr))
	store.file = writer

	err := store.append([]byte("tail"))
	require.ErrorIs(t, err, writeErr)
	text, metadata, finishErr := store.finish(-1, err)

	require.ErrorIs(t, finishErr, writeErr)
	assert.True(t, metadata.Truncated)
	assert.Empty(t, metadata.FullOutputPath)
	assert.NoFileExists(t, path)
	assert.Contains(t, text, "Complete output capture failed")
	assert.NotContains(t, text, "Full output:")
}

// TestOutputStoreRemovesFileAfterCloseFailure keeps timeout output without advertising the failed file.
func TestOutputStoreRemovesFileAfterCloseFailure(t *testing.T) {
	t.Parallel()

	store := newOutputStore()
	raw := []byte(strings.Repeat("x", textbudget.MaximumBytes+1))
	require.NoError(t, store.append(raw))
	store.appendText(string(raw))
	path := store.path
	t.Cleanup(func() { _ = os.Remove(path) })
	require.NoError(t, store.file.Close())
	closeErr := errors.New("delayed storage failure")
	file := NewMockOutputFile(gomock.NewController(t))
	file.EXPECT().Close().Return(closeErr)
	store.file = file
	timeoutErr := errors.New("bash command timed out after 1 seconds")

	text, metadata, err := store.finish(-1, timeoutErr)

	require.ErrorIs(t, err, closeErr)
	assert.True(t, metadata.Truncated)
	assert.Empty(t, metadata.FullOutputPath)
	assert.NoFileExists(t, path)
	assert.Contains(t, text, "Complete output capture failed")
	assert.Contains(t, text, timeoutErr.Error())
	assert.NotContains(t, text, "Full output:")
}

// TestOutputStoreUsesCompleteTerminalBudget spills only when status makes the result exceed a limit.
func TestOutputStoreUsesCompleteTerminalBudget(t *testing.T) {
	t.Parallel()

	statusOverhead := len("\n\n[Exit code: 0]\n")
	tests := []struct {
		name     string
		boundary []byte
		excess   []byte
	}{
		{
			name:     "byte limit",
			boundary: []byte(strings.Repeat("x", textbudget.MaximumBytes-statusOverhead)),
			excess:   []byte(strings.Repeat("x", textbudget.MaximumBytes-statusOverhead+1)),
		},
		{
			name:     "line limit",
			boundary: []byte(strings.Repeat("\n", textbudget.MaximumLines-3)),
			excess:   []byte(strings.Repeat("\n", textbudget.MaximumLines-2)),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			within := newOutputStore()
			require.NoError(t, within.append(testCase.boundary))
			within.appendText(string(testCase.boundary))
			text, metadata, err := within.finish(0, nil)
			require.NoError(t, err)
			assert.False(t, metadata.Truncated)
			assert.True(t, withinTextBudget(text))

			over := newOutputStore()
			require.NoError(t, over.append(testCase.excess))
			over.appendText(string(testCase.excess))
			text, metadata, err = over.finish(0, nil)
			require.NoError(t, err)
			require.True(t, metadata.Truncated)
			t.Cleanup(func() { require.NoError(t, os.Remove(metadata.FullOutputPath)) })
			assert.True(t, withinTextBudget(text))
			complete, err := os.ReadFile(metadata.FullOutputPath)
			require.NoError(t, err)
			assert.Equal(t, testCase.excess, complete)
		})
	}
}
