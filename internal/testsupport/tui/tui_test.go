package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	cleanupHelperEnvironment = "GLYPH_TUI_CLEANUP_HELPER"
	cleanupGroupsEnvironment = "GLYPH_TUI_CLEANUP_GROUPS"
	cleanupFIFOEnvironment   = "GLYPH_TUI_CLEANUP_FIFO"
	cleanupHelperTimeout     = 5 * time.Second
)

// TestEarlyFailureCleanup verifies cleanup after the helper test context is canceled.
func TestEarlyFailureCleanup(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY cleanup runs on Darwin arm64")
	}

	// Arrange a detached helper process with process-group ownership, output waiters, and registered cleanup.
	helperDirectory := t.TempDir()
	groupsPath := filepath.Join(helperDirectory, "groups")
	fifoPath := filepath.Join(helperDirectory, "hold")
	helperContext, cancelHelper := context.WithTimeout(t.Context(), cleanupHelperTimeout)
	defer cancelHelper()
	helperLifetime, cancelHelperProcess := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(
		helperLifetime, os.Args[0], "-test.run=^TestEarlyFailureCleanupHelper$",
	)
	command.Env = append(
		os.Environ(),
		cleanupHelperEnvironment+"=1",
		cleanupGroupsEnvironment+"="+groupsPath,
		cleanupFIFOEnvironment+"="+fifoPath,
	)
	ConfigureProcessGroup(command)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	observer := NewOutputObserver(helperContext)
	command.Stderr = observer
	require.NoError(t, command.Start())
	commandWaiter := NewCommandWaiter(command)
	outputWaiter := NewOutputWaiter(observer, output)
	cleanup := ProcessGroupCleanup{
		Cancel:        cancelHelperProcess,
		Input:         input,
		Command:       command,
		CommandWaiter: commandWaiter,
		OutputWaiter:  outputWaiter,
		Timeout:       cleanupHelperTimeout,
	}
	RegisterProcessGroupCleanup(t.Context(), t, cleanup)

	// Act by waiting for the forced helper failure, completing output copy, and loading its recorded process groups.
	runErr := commandWaiter.Wait(helperContext)
	copyErr := outputWaiter.Wait(helperContext)
	var timeoutCleanupErr error
	if helperContext.Err() != nil {
		// The helper lifetime stays active so timeout cleanup can still snapshot all descendants.
		timeoutCleanupErr = cleanupProcessGroup(helperContext, cleanup)
	}
	processGroups, groupsErr := loadProcessGroups(groupsPath)
	if len(processGroups) > 0 {
		// Emergency cleanup only prevents a failed stop assertion from leaking recorded groups.
		defer func() {
			emergencyErr := errors.Join(
				terminateProcessGroups(processGroups),
				checkProcessGroupsStopped(processGroups),
			)
			if emergencyErr != nil {
				t.Errorf("emergency process-group cleanup: %v", emergencyErr)
			}
		}()
	}

	// Assert cleanup completes without timeout and every recorded process group no longer exists.
	require.NoError(t, groupsErr, observer.String())
	require.GreaterOrEqual(t, len(processGroups), 2)
	require.NoError(t, timeoutCleanupErr, observer.String())
	require.Error(t, runErr, observer.String())
	require.NoError(t, helperContext.Err(), observer.String())
	require.NoError(t, copyErr, observer.String())
	for _, processGroupID := range processGroups {
		require.ErrorIs(t, syscall.Kill(-processGroupID, 0), syscall.ESRCH)
	}
}

// TestEarlyFailureCleanupHelper leaves a live PTY process tree when the test body fails.
func TestEarlyFailureCleanupHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv(cleanupHelperEnvironment) == "" {
		return
	}

	// Arrange a detached PTY process tree with observers and cleanup that survives test-context cancellation.
	// Registered cleanup owns wrapper cancellation after testing cancels the helper context.
	commandContext, cancelCommand := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(
		commandContext, "/usr/bin/script", "-q", "/dev/null",
		"/bin/sh", "-c",
		"trap '' HUP TERM; mkfifo \"$GLYPH_TUI_CLEANUP_FIFO\"; cat \"$GLYPH_TUI_CLEANUP_FIFO\" & echo ready; wait",
	)
	ConfigureProcessGroup(command)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	observerContext, cancelObserver := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancelObserver)
	observer := NewOutputObserver(observerContext)
	require.NoError(t, command.Start())
	commandWaiter := NewCommandWaiter(command)
	outputWaiter := NewOutputWaiter(observer, output)
	RegisterProcessGroupCleanup(t.Context(), t, ProcessGroupCleanup{
		Cancel:        cancelCommand,
		Input:         input,
		Command:       command,
		CommandWaiter: commandWaiter,
		OutputWaiter:  outputWaiter,
		Timeout:       time.Second,
	})

	// Act by waiting for the wrapper, discovering its process groups, and publishing them for the parent test.
	observer.WaitNext(t, "ready")
	processGroups, err := discoverProcessGroups(observerContext, command.Process.Pid)

	// Assert the live tree contains the wrapper and at least one descendant before cleanup.
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(processGroups), 2)
	writeProcessGroups(t, os.Getenv(cleanupGroupsEnvironment), processGroups)

	// Act by forcing failure so testing cancels t.Context before it runs the registered cleanup.
	t.FailNow()
}

// writeProcessGroups publishes the process groups before the helper test fails.
func writeProcessGroups(t *testing.T, path string, processGroups []int) {
	t.Helper()
	values := make([]string, 0, len(processGroups))
	for _, processGroupID := range processGroups {
		values = append(values, strconv.Itoa(processGroupID))
	}
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(values, "\n")), 0o600))
}

// loadProcessGroups returns partial records with any read or parse error for emergency cleanup.
func loadProcessGroups(path string) ([]int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read process groups: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	processGroups := make([]int, 0, len(lines))
	for _, line := range lines {
		processGroupID, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			return processGroups, fmt.Errorf("parse process group %q: %w", line, parseErr)
		}
		processGroups = append(processGroups, processGroupID)
	}
	return processGroups, nil
}
