//go:build integration

package bash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestServiceRun streams both channels and returns complete nonzero-exit output.
func TestServiceRun(t *testing.T) {
	t.Parallel()

	events := make([]bashusecase.Stream, 0, 2)
	fragments := make([]string, 0, 2)
	result, err := New().Run(t.Context(), "printf out; printf err >&2; exit 7", func(stream bashusecase.Stream, content string) error {
		events = append(events, stream)
		fragments = append(fragments, content)
		return nil
	})

	require.NoError(t, err)
	assert.ElementsMatch(t, []bashusecase.Stream{bashusecase.StreamStdout, bashusecase.StreamStderr}, events)
	assert.Equal(t, strings.Join(fragments, "")+"\n\n[Exit code: 7]\n", result.Output)
	assert.False(t, result.Truncation.Truncated)
}

// TestServiceRunKeepsRawInvalidUTF8 stores original bytes while streaming valid text.
func TestServiceRunKeepsRawInvalidUTF8(t *testing.T) {
	t.Parallel()

	validProgress := true
	result, err := New().Run(t.Context(), `printf '\377'; printf '%060000d' 0`, func(_ bashusecase.Stream, content string) error {
		validProgress = validProgress && utf8.ValidString(content)
		return nil
	})

	require.NoError(t, err)
	assert.True(t, validProgress)
	require.True(t, result.Truncation.Truncated)
	t.Cleanup(func() { require.NoError(t, os.Remove(result.Truncation.FullOutputPath)) })
	complete, err := os.ReadFile(result.Truncation.FullOutputPath)
	require.NoError(t, err)
	require.Len(t, complete, 60001)
	assert.Equal(t, byte(0xff), complete[0])
	assert.Equal(t, strings.Repeat("0", 60000), string(complete[1:]))
}

// TestServiceRunBoundsOutput keeps the visible tail bounded and preserves complete raw output.
func TestServiceRunBoundsOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "line limit",
			command: `i=0; while [ "$i" -lt 2101 ]; do printf 'line\n'; i=$((i+1)); done`,
			want:    strings.Repeat("line\n", 2101),
		},
		{
			name:    "byte limit",
			command: `printf '%060000d' 0`,
			want:    strings.Repeat("0", 60000),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := New().Run(t.Context(), testCase.command, func(bashusecase.Stream, string) error {
				return nil
			})

			require.NoError(t, err)
			require.True(t, result.Truncation.Truncated)
			require.NotEmpty(t, result.Truncation.FullOutputPath)
			t.Cleanup(func() { require.NoError(t, os.Remove(result.Truncation.FullOutputPath)) })
			complete, err := os.ReadFile(result.Truncation.FullOutputPath)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, string(complete))
			assert.LessOrEqual(t, len(result.Output), textbudget.MaximumBytes)
			assert.LessOrEqual(t, strings.Count(result.Output, "\n"), textbudget.MaximumLines)
			assert.Contains(t, result.Output, "Full output: "+result.Truncation.FullOutputPath)
		})
	}
}

// TestServiceRunPreservesTimeoutCause returns partial output with the timeout outcome.
func TestServiceRunPreservesTimeoutCause(t *testing.T) {
	t.Parallel()

	timeoutErr := errors.New("bash command timed out after 0.01 seconds")
	ctx, cancel := context.WithCancelCause(t.Context())
	started := make(chan struct{})
	var startedOnce sync.Once
	outcome := make(chan struct {
		result bashusecase.ProcessResult
		err    error
	}, 1)
	go func() {
		result, err := New().Run(ctx, "printf started; exec sleep 30", func(_ bashusecase.Stream, _ string) error {
			startedOnce.Do(func() { close(started) })
			return nil
		})
		outcome <- struct {
			result bashusecase.ProcessResult
			err    error
		}{result: result, err: err}
	}()
	<-started
	cancel(timeoutErr)
	result := <-outcome

	require.ErrorIs(t, result.err, timeoutErr)
	assert.Contains(t, result.result.Output, "started")
	assert.Contains(t, result.result.Output, timeoutErr.Error())
}

// TestServiceRunPreservesPreflightCause distinguishes timeout from caller cancellation.
func TestServiceRunPreservesPreflightCause(t *testing.T) {
	t.Parallel()

	timeoutErr := errors.New("bash command timed out before process start")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(timeoutErr)

	_, err := New().Run(ctx, "printf unreachable", func(bashusecase.Stream, string) error { return nil })

	require.ErrorIs(t, err, timeoutErr)
	assert.NotErrorIs(t, err, context.Canceled)
}

// TestServiceRunCancellation kills the active process group and preserves cancellation.
func TestServiceRunCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	var startedOnce sync.Once
	outcome := make(chan error, 1)
	go func() {
		_, err := New().Run(ctx, "printf started; exec sleep 30", func(_ bashusecase.Stream, _ string) error {
			startedOnce.Do(func() { close(started) })
			return nil
		})
		outcome <- err
	}()
	select {
	case err := <-outcome:
		require.ErrorIs(t, err, context.Canceled)
	case <-started:
		cancel()
		require.ErrorIs(t, <-outcome, context.Canceled)
	}
}

// TestServiceRunCancellationKillsDescendants proves background processes receive the group SIGKILL.
func TestServiceRunCancellationKillsDescendants(t *testing.T) {
	t.Parallel()

	marker := t.TempDir() + "/survived"
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	var startedOnce sync.Once
	outcome := make(chan error, 1)
	command := fmt.Sprintf("(printf started; sleep 0.2; touch %q) & wait", marker)
	go func() {
		_, err := New().Run(ctx, command, func(_ bashusecase.Stream, _ string) error {
			startedOnce.Do(func() { close(started) })
			return nil
		})
		outcome <- err
	}()
	<-started
	cancel()

	require.ErrorIs(t, <-outcome, context.Canceled)
	// Wait past the descendant deadline so a parent-only kill cannot satisfy the assertion.
	time.Sleep(300 * time.Millisecond)
	assert.NoFileExists(t, marker)
}
