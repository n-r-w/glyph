//go:build integration

package bash

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// processOutcome transfers a fully joined Run result to the assertion goroutine.
type processOutcome struct {
	// result contains bounded output and the shell exit code.
	result bashusecase.ProcessResult
	// err retains all operation failures.
	err error
}

// TestCancellationOutputLifetimes checks live descendants with inherited and redirected descriptors.
func TestCancellationOutputLifetimes(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"caller", "timeout", "consumer"} {
		for _, lifetime := range []string{"inherited", "redirected", "shell exited"} {
			t.Run(mode+"/"+lifetime, func(t *testing.T) {
				t.Parallel()
				// Arrange a known live descendant and an independently observable shell lifetime.
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				ready := make(chan string, 1)
				release := make(chan struct{})
				done := make(chan processOutcome, 1)
				command := `sleep 30 & printf '%s %s\n' "$$" "$!"; wait`
				if lifetime == "redirected" {
					command = `sleep 30 >/dev/null 2>&1 & printf '%s %s\n' "$$" "$!"; wait`
				} else if lifetime == "shell exited" {
					command = `sleep 30 & printf '%s %s\n' "$$" "$!"; exit 0`
				}
				cause := context.Canceled
				if mode == "timeout" {
					cause = errors.New("bash command timed out after 0.01 seconds")
				}
				if mode == "consumer" {
					cause = errors.New("output consumer failed Ω")
				}
				go func() {
					result, err := New().Run(ctx, command, func(_ bashusecase.Stream, content string) error {
						ready <- content
						<-release
						if mode == "consumer" {
							return cause
						}
						return nil
					})
					done <- processOutcome{result: result, err: err}
				}()
				var identity string
				select {
				case identity = <-ready:
				case outcome := <-done:
					t.Fatalf("shell did not start: %v", outcome.err)
				case <-time.After(5 * time.Second):
					t.Fatal("shell did not report child identity")
				}
				fields := strings.Fields(identity)
				require.Len(t, fields, 2)
				pgid, child := processNumber(t, fields[0]), processNumber(t, fields[1])
				t.Cleanup(func() { cleanupGroup(t, pgid) })
				require.Contains(t, runningGroupMembers(t, pgid), child)
				if lifetime == "shell exited" {
					require.Eventually(t, func() bool {
						return !slices.Contains(runningGroupMembers(t, pgid), pgid)
					}, time.Second, time.Millisecond, "shell must exit before cancellation while its child holds output")
				}
				// Act with caller cancellation, a timeout cause, or a callback-only failure.
				if mode == "timeout" {
					timer := time.AfterFunc(10*time.Millisecond, func() { cancel(cause) })
					defer timer.Stop()
				} else if mode == "caller" {
					cancel(cause)
				}
				close(release)
				var outcome processOutcome
				completed := false
				select {
				case outcome = <-done:
					completed = true
				case <-time.After(time.Second):
				}
				// Assert completion and descendant state independently, before any cleanup signal.
				assert.True(t, completed, "process operation did not complete")
				assert.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
					time.Second, time.Millisecond, "descendant remains live after cancellation")
				if !completed {
					cleanupGroup(t, pgid)
					select {
					case outcome = <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("operation did not join after test cleanup")
					}
				}
				require.ErrorIs(t, outcome.err, cause)
				if mode == "caller" {
					assert.Empty(t, outcome.result.Output)
				} else {
					assert.Contains(t, outcome.result.Output, identity)
					assert.Contains(t, outcome.result.Output, cause.Error())
					assert.NotErrorIs(t, outcome.err, context.Canceled)
				}
			})
		}
	}
}

// TestNormalShellExitDrainsOutput retains output written by a child after shell exit.
func TestNormalShellExitDrainsOutput(t *testing.T) {
	t.Parallel()
	// Arrange a shell that exits before its descendant closes inherited output.
	fragments := make([]string, 0)
	// Act without cancellation so the adapter must drain rather than close its readers early.
	result, err := New().Run(t.Context(), `(sleep 0.05; printf tail) & printf head; exit 7`,
		func(_ bashusecase.Stream, content string) error { fragments = append(fragments, content); return nil })
	// Assert normal nonzero exit and complete delivery-ordered output.
	require.NoError(t, err)
	assert.Equal(t, 7, result.ExitCode)
	assert.Equal(t, "headtail", strings.Join(fragments, ""))
	assert.Equal(t, "headtail\n\n[Exit code: 7]\n", result.Output)
}

// TestCancellationExactCommand repeats the original command with exact test-owned group cleanup.
//
//nolint:paralleltest // Direct-child discovery requires serial execution.
func TestCancellationExactCommand(t *testing.T) {
	// This test runs serially so the direct shell child is unambiguous in the process table.
	for attempt := range 100 {
		// Arrange the exact observed command and a callback failure concurrent with cancellation.
		ctx, cancel := context.WithCancel(t.Context())
		ready := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		cause := errors.New("exact-command progress failure")
		go func() {
			_, err := New().Run(ctx, "printf started; sleep 30", func(bashusecase.Stream, string) error {
				close(ready)
				<-release
				cancel()
				return cause
			})
			done <- err
		}()
		select {
		case <-ready:
		case err := <-done:
			t.Fatalf("shell setup failed: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("shell setup did not complete")
		}
		groups := make([]int, 0)
		for _, process := range observeProcesses(t) {
			if process.parent == os.Getpid() && process.group == process.pid {
				groups = append(groups, process.group)
			}
		}
		require.Len(t, groups, 1)
		pgid := groups[0]
		// Act only after recording the exact group that test cleanup may signal.
		close(release)
		completed := false
		var runErr error
		select {
		case runErr = <-done:
			completed = true
		case <-time.After(time.Second):
		}
		// Assert the operation and actual descendants finish before cleanup sends any signal.
		assert.True(t, completed, "attempt=%d group=%d operation did not complete", attempt, pgid)
		assert.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
			time.Second, time.Millisecond, "attempt=%d group=%d remains live", attempt, pgid)
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
		require.ErrorIs(t, runErr, cause)
		if t.Failed() {
			return
		}
	}
}
