//go:build integration

package bash

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// processObservation contains only the identity and execution state needed by process assertions.
type processObservation struct {
	// pid identifies the process.
	pid int
	// parent identifies the process that started this process.
	parent int
	// group identifies the process group.
	group int
	// state distinguishes running work from a terminated zombie.
	state string
}

// observeProcesses reads native process state without sending signals.
func observeProcesses(t *testing.T) []processObservation {
	t.Helper()
	if runtime.GOOS == "linux" {
		return observeLinuxProcesses(t)
	}
	// Test cleanup runs after its context is canceled, but each native snapshot still has a fixed bound.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=,stat=").Output()
	require.NoError(t, err)
	observations := make([]processObservation, 0)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		observations = append(observations, processObservation{
			pid: processNumber(t, fields[0]), parent: processNumber(t, fields[1]),
			group: processNumber(t, fields[2]), state: fields[3],
		})
	}
	return observations
}

// observeLinuxProcesses reads procfs because the verification image has no ps executable.
func observeLinuxProcesses(t *testing.T) []processObservation {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	require.NoError(t, err)
	observations := make([]processObservation, 0)
	for _, entry := range entries {
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil {
			continue
		}
		data, readErr := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if errors.Is(readErr, os.ErrNotExist) || errors.Is(readErr, syscall.ESRCH) {
			continue
		}
		require.NoError(t, readErr)
		fields := strings.Fields(string(data)[bytes.LastIndexByte(data, ')')+1:])
		require.GreaterOrEqual(t, len(fields), 3)
		observations = append(observations, processObservation{
			pid: pid, parent: processNumber(t, fields[1]), group: processNumber(t, fields[2]), state: fields[0],
		})
	}
	return observations
}

// processNumber parses one numeric process-table field.
func processNumber(t *testing.T, value string) int {
	t.Helper()
	number, err := strconv.Atoi(value)
	require.NoError(t, err)
	return number
}

// runningGroupMembers observes live group members, not terminated zombies.
func runningGroupMembers(t *testing.T, pgid int) []int {
	t.Helper()
	members := make([]int, 0)
	for _, process := range observeProcesses(t) {
		if process.group == pgid && !strings.HasPrefix(process.state, "Z") && process.state != "X" {
			members = append(members, process.pid)
		}
	}
	return members
}
