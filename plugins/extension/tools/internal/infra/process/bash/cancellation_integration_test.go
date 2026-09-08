//go:build integration

package bash

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestCancellationForkRace repeats cancellation while the shell starts an ordinary descendant.
func TestCancellationForkRace(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"", " >/dev/null 2>&1"} {
		t.Run("sleep 30"+suffix, func(t *testing.T) {
			t.Parallel()
			// Arrange a real shell, its reported process group, and an independent callback failure.
			progressErr := errors.New("progress failed during descendant cancellation Ω")
			for attempt := range 100 {
				ctx, cancel := context.WithCancel(t.Context())
				group := make(chan int, 1)
				done := make(chan error, 1)
				go func() {
					_, err := New().Run(ctx, `printf '%s\n' "$$"; sleep 30`+suffix, func(_ bashusecase.Stream, content string) error {
						pid, parseErr := strconv.Atoi(strings.TrimSpace(content))
						if parseErr != nil {
							return parseErr
						}
						group <- pid
						cancel()
						return progressErr
					})
					done <- err
				}()
				// Act by canceling from output delivery while the shell can still fork sleep.
				var pgid int
				select {
				case pgid = <-group:
				case <-time.After(5 * time.Second):
					cancel()
					t.Fatal("shell setup did not complete")
				}
				completed := false
				var runErr error
				select {
				case runErr = <-done:
					completed = true
				case <-time.After(time.Second):
				}
				// Assert both operation completion and actual termination before test cleanup signals.
				assert.True(
					t,
					completed,
					"attempt=%d group=%d operation did not complete after cancellation",
					attempt,
					pgid,
				)
				// Signal delivery can outlast Run, so observe actual termination without sending another signal.
				assert.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
					time.Second, time.Millisecond, "attempt=%d group=%d has running descendants", attempt, pgid)
				cleanupGroup(t, pgid)
				if !completed {
					select {
					case runErr = <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("operation did not join after test cleanup")
					}
				}
				cancel()
				require.ErrorIs(t, runErr, context.Canceled)
				require.ErrorIs(t, runErr, progressErr)
				if t.Failed() {
					return
				}
			}
		})
	}
}

// cleanupGroup stops only the process group reported by this test's shell.
func cleanupGroup(t *testing.T, pgid int) {
	t.Helper()
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return
	}
	if errors.Is(err, syscall.EPERM) {
		// A zombie-only group can reject the signal. Accept it only after observing no live members.
		require.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
			time.Second, time.Millisecond, "cleanup group %d still has live members after signal error: %v", pgid, err)
		return
	}
	require.NoError(t, err, "cleanup group %d", pgid)
}
