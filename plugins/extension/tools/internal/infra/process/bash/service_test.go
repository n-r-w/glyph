package bash

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestServiceRun streams both channels and returns complete nonzero-exit output.
func TestServiceRun(t *testing.T) {
	t.Parallel()

	events := make([]bashusecase.Stream, 0, 2)
	result, err := New().Run(t.Context(), "printf out; printf err >&2; exit 7", func(stream bashusecase.Stream, _ string) error {
		events = append(events, stream)
		return nil
	})

	require.NoError(t, err)
	assert.ElementsMatch(t, []bashusecase.Stream{bashusecase.StreamStdout, bashusecase.StreamStderr}, events)
	assert.Equal(t, bashusecase.ProcessResult{Stdout: "out", Stderr: "err", ExitCode: 7}, result)
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
	assert.NoFileExists(t, marker)
}
