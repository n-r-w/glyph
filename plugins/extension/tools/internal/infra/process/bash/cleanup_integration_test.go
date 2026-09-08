//go:build integration

package bash

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCleanupGroupAcceptsZombie does not treat an already terminated group as failed test cleanup.
func TestCleanupGroupAcceptsZombie(t *testing.T) {
	t.Parallel()
	// Arrange a test-owned group whose only member has exited but is not reaped yet.
	command := startCleanupCommand(t, "exit 0")
	pgid := command.Process.Pid
	require.Eventually(t, func() bool {
		for _, process := range observeProcesses(t) {
			if process.pid == pgid {
				return strings.HasPrefix(process.state, "Z")
			}
		}
		return false
	}, time.Second, time.Millisecond, "child must become a zombie before cleanup is tested")
	// Act in test cleanup, after testing has canceled the test context but before the child is reaped.
	t.Cleanup(func() {
		require.ErrorIs(t, t.Context().Err(), context.Canceled)
		cleanupGroup(t, pgid)
		// Assert that cleanup accepts only a group with no live members.
		require.Empty(t, runningGroupMembers(t, pgid))
	})
}

// TestCleanupGroupStopsLiveMember verifies that the cleanup helper still terminates live work.
func TestCleanupGroupStopsLiveMember(t *testing.T) {
	t.Parallel()
	// Arrange a live test-owned group whose command cannot finish within the assertion interval.
	command := startCleanupCommand(t, "exec sleep 30")
	pgid := command.Process.Pid
	require.Contains(t, runningGroupMembers(t, pgid), pgid)
	// Act through the shared test cleanup helper.
	cleanupGroup(t, pgid)
	// Assert actual termination before the test's fallback cleanup can send another signal.
	require.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
		time.Second, time.Millisecond, "cleanup left group %d live", pgid)
}

// startCleanupCommand creates an isolated test group and registers its sole wait and fallback kill.
func startCleanupCommand(t *testing.T, text string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(context.WithoutCancel(t.Context()), "bash", "-c", text)
	command.SysProcAttr = &syscall.SysProcAttr{
		Chroot: "", Credential: nil, Ptrace: false, Setsid: false, Setpgid: true,
		Setctty: false, Noctty: false, Ctty: 0, Foreground: false, Pgid: 0,
	}
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		killErr := command.Process.Kill()
		if !errors.Is(killErr, os.ErrProcessDone) {
			require.NoError(t, killErr)
		}
		waitErr := command.Wait()
		if _, ok := errors.AsType[*exec.ExitError](waitErr); !ok {
			require.NoError(t, waitErr)
		}
	})
	return command
}
