//go:build integration

package bash

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// timeoutOutcome transfers the usecase result and its completed progress stream to the assertions.
type timeoutOutcome struct {
	// result contains the model-visible timeout result.
	result extensioncontroller.BashResult
	// err contains timeout and any independent process failure.
	err error
	// progress contains all callbacks before Execute returned.
	progress []extensioncontroller.BashProgress
}

// TestServiceTimeoutTerminatesProcess checks real usecase timeouts and descendant state on both output lifetimes.
func TestServiceTimeoutTerminatesProcess(t *testing.T) {
	t.Parallel()
	for _, redirect := range []string{"", " >/dev/null 2>&1"} {
		t.Run("sleep 30"+redirect, func(t *testing.T) {
			t.Parallel()
			// Arrange the real timeout owner and a shell that reports its group and descendant identities.
			service := bashusecase.New(New())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := make(chan string, 1)
			done := make(chan timeoutOutcome, 1)
			command := "sleep 30" + redirect + ` & printf 'started %s %s' "$$" "$!"; wait`
			// Act through Execute so the production timeout policy starts and cancels the process group.
			go func() {
				progress := make([]extensioncontroller.BashProgress, 0)
				result, err := service.Execute(ctx, extensioncontroller.BashCommand{
					Text: command, Timeout: mo.Some(0.1),
				}, func(event extensioncontroller.BashProgress) error {
					progress = append(progress, event)
					if event.Channel == extensioncontroller.BashProgressStdout {
						ready <- event.Content
					}
					return nil
				})
				done <- timeoutOutcome{result: result, err: err, progress: progress}
			}()
			var identity string
			select {
			case identity = <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("shell did not report its descendant")
			}
			fields := strings.Fields(identity)
			require.Len(t, fields, 3)
			require.Equal(t, "started", fields[0])
			pgid, child := processNumber(t, fields[1]), processNumber(t, fields[2])
			t.Cleanup(func() { cleanupGroup(t, pgid) })
			var outcome timeoutOutcome
			completed := false
			select {
			case outcome = <-done:
				completed = true
			case <-time.After(time.Second):
			}
			// Assert operation completion and actual descendant termination before any test cleanup signal.
			assert.True(t, completed, "timeout operation did not complete for group %d", pgid)
			assert.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
				time.Second, time.Millisecond, "timeout left live group %d with descendant %d", pgid, child)
			if !completed {
				cleanupGroup(t, pgid)
				cancel()
				select {
				case outcome = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("timeout operation did not join after test cleanup")
				}
			}
			// Assert the complete error allowlist, exact timeout text, and the unchanged progress contract.
			const timeoutText = "bash command timed out after 0.1 seconds"
			expectedError := "run bash command: " + timeoutText
			if errors.Is(outcome.err, syscall.EPERM) {
				t.Logf("retained independent group signal source: %v", outcome.err)
				require.Equal(t, "darwin", runtime.GOOS, "EPERM is not expected for this Linux fixture")
				expectedError += "\nkill bash process group: " + syscall.EPERM.Error()
			}
			require.EqualError(t, outcome.err, expectedError)
			require.NotErrorIs(t, outcome.err, context.Canceled)
			require.Equal(t, identity+"\n\n["+timeoutText+"]\n", outcome.result.Text)
			require.Equal(t, -1, outcome.result.ExitCode)
			require.False(t, outcome.result.Truncation.Truncated)
			require.GreaterOrEqual(t, len(outcome.progress), 2)
			require.Equal(t, extensioncontroller.BashProgress{
				Channel: extensioncontroller.BashProgressStatus, Content: "running",
			}, outcome.progress[0])
			fragments := make([]string, 0, len(outcome.progress)-1)
			for _, event := range outcome.progress[1:] {
				require.Equal(t, extensioncontroller.BashProgressStdout, event.Channel)
				fragments = append(fragments, event.Content)
			}
			require.Equal(t, identity, strings.Join(fragments, ""))
		})
	}
}
