// Package tui provides terminal process helpers for tests that use a real pseudo-terminal.
package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// SetTerminalSize applies the dimensions used by terminal rendering assertions.
func SetTerminalSize(t *testing.T, terminalFile *os.File, width, height int) {
	t.Helper()
	//nolint:gosec // Tests control numeric dimensions passed to the fixed stty executable.
	command := exec.CommandContext(
		t.Context(), "/bin/stty", "rows", strconv.Itoa(height), "columns", strconv.Itoa(width),
	)
	command.Stdin = terminalFile
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

// Write sends one raw keyboard sequence to a pseudo-terminal.
func Write(t *testing.T, writer io.Writer, content string) {
	t.Helper()
	_, err := io.WriteString(writer, content)
	require.NoError(t, err)
}

// OutputObserver records terminal bytes and waits for ordered rendering fragments.
type OutputObserver struct {
	context      context.Context
	mutex        sync.Mutex
	content      bytes.Buffer
	notification chan struct{}
	cursor       int
}

var _ io.Writer = (*OutputObserver)(nil)

// NewOutputObserver creates an ordered terminal output cursor bound to ctx.
func NewOutputObserver(ctx context.Context) *OutputObserver {
	return &OutputObserver{
		context:      ctx,
		notification: make(chan struct{}, 1),
		mutex:        sync.Mutex{},
		content:      bytes.Buffer{},
		cursor:       0,
	}
}

// Write records terminal bytes and wakes blocked rendering assertions.
func (observer *OutputObserver) Write(content []byte) (int, error) {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	written, err := observer.content.Write(content)
	select {
	case observer.notification <- struct{}{}:
	default:
	}
	return written, err
}

// WaitNext requires one rendering fragment after the observer cursor.
func (observer *OutputObserver) WaitNext(t *testing.T, expected string) {
	t.Helper()
	for {
		observer.mutex.Lock()
		content := observer.content.String()
		position := strings.Index(content[observer.cursor:], expected)
		if position >= 0 {
			observer.cursor += position + len(expected)
			observer.mutex.Unlock()
			return
		}
		observer.mutex.Unlock()

		select {
		case <-observer.notification:
		case <-observer.context.Done():
			t.Fatalf("terminal output did not contain %q after cursor:\n%s", expected, content)
		}
	}
}

// WaitForOutputAfter waits until the terminal writes bytes after one checkpoint.
func (observer *OutputObserver) WaitForOutputAfter(t *testing.T, checkpoint int) {
	t.Helper()
	for {
		observer.mutex.Lock()
		advanced := observer.content.Len() > checkpoint
		content := observer.content.String()
		observer.mutex.Unlock()
		if advanced {
			return
		}
		select {
		case <-observer.notification:
		case <-observer.context.Done():
			t.Fatalf("terminal output did not advance after checkpoint:\n%s", content)
		}
	}
}

// Checkpoint returns the current output boundary for later state-sensitive assertions.
func (observer *OutputObserver) Checkpoint() int {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	return observer.content.Len()
}

// StringFrom returns output captured at or after one checkpoint.
func (observer *OutputObserver) StringFrom(checkpoint int) string {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	if checkpoint < 0 || checkpoint > observer.content.Len() {
		return ""
	}
	return observer.content.String()[checkpoint:]
}

// String returns a stable copy of all recorded terminal output.
func (observer *OutputObserver) String() string {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	return observer.content.String()
}

// OutputWaiter stores one output-copy result for normal and cleanup paths.
type OutputWaiter struct {
	result error
	done   chan struct{}
}

// NewOutputWaiter copies terminal output and starts the only wait for that copy operation.
func NewOutputWaiter(writer io.Writer, reader io.Reader) *OutputWaiter {
	waiter := &OutputWaiter{
		done:   make(chan struct{}),
		result: nil,
	}
	go func() {
		_, waiter.result = io.Copy(writer, reader)
		close(waiter.done)
	}()
	return waiter
}

// Wait joins the output copy or reports that ctx expired before the join completed.
func (waiter *OutputWaiter) Wait(ctx context.Context) error {
	select {
	case <-waiter.done:
		return waiter.result
	default:
	}
	select {
	case <-waiter.done:
		return waiter.result
	case <-ctx.Done():
		return fmt.Errorf("wait for terminal output copy: %w", ctx.Err())
	}
}

// CommandWaiter stores one subprocess wait result for normal and cleanup paths.
type CommandWaiter struct {
	result error
	done   chan struct{}
}

// NewCommandWaiter starts the only Wait call for command.
func NewCommandWaiter(command *exec.Cmd) *CommandWaiter {
	waiter := &CommandWaiter{
		done:   make(chan struct{}),
		result: nil,
	}
	go func() {
		waiter.result = command.Wait()
		close(waiter.done)
	}()
	return waiter
}

// Wait joins the subprocess or reports that ctx expired before the join completed.
func (waiter *CommandWaiter) Wait(ctx context.Context) error {
	select {
	case <-waiter.done:
		return waiter.result
	default:
	}
	select {
	case <-waiter.done:
		return waiter.result
	case <-ctx.Done():
		return fmt.Errorf("wait for terminal subprocess: %w", ctx.Err())
	}
}

// ProcessGroupCleanup contains the resources owned by one outer pseudo-terminal process.
type ProcessGroupCleanup struct {
	Cancel        context.CancelFunc
	Input         io.Closer
	Command       *exec.Cmd
	CommandWaiter *CommandWaiter
	OutputWaiter  *OutputWaiter
	Timeout       time.Duration
}

// ConfigureProcessGroup isolates command so cleanup signals cannot reach the test process.
func ConfigureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		Chroot:     "",
		Credential: nil,
		Ptrace:     false,
		Setsid:     false,
		Setpgid:    true,
		Setctty:    false,
		Noctty:     false,
		Ctty:       0,
		Foreground: false,
		Pgid:       0,
	}
}

// RegisterProcessGroupCleanup terminates and joins all resources after normal or failed tests.
func RegisterProcessGroupCleanup(baseContext context.Context, t *testing.T, cleanup ProcessGroupCleanup) {
	t.Helper()
	t.Cleanup(func() {
		if err := cleanupProcessGroup(baseContext, cleanup); err != nil {
			t.Errorf("clean pseudo-terminal process group: %v", err)
		}
	})
}

// cleanupProcessGroup replaces test cancellation with one active bound before it stops and joins resources.
func cleanupProcessGroup(baseContext context.Context, cleanup ProcessGroupCleanup) error {
	cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(baseContext), cleanup.Timeout)
	defer cancelCleanup()

	rootProcessGroup := cleanup.Command.Process.Pid
	processGroups, discoveryErr := discoverProcessGroups(cleanupContext, rootProcessGroup)
	if !slices.Contains(processGroups, rootProcessGroup) {
		processGroups = append(processGroups, rootProcessGroup)
	}

	var cleanupErr error
	if closeErr := cleanup.Input.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close pseudo-terminal input: %w", closeErr))
	}
	cleanup.Cancel()
	cleanupErr = errors.Join(cleanupErr, discoveryErr, terminateProcessGroups(processGroups))

	waitErr := cleanup.CommandWaiter.Wait(cleanupContext)
	if errors.Is(waitErr, context.DeadlineExceeded) {
		cleanupErr = errors.Join(cleanupErr, waitErr)
	}
	if copyErr := cleanup.OutputWaiter.Wait(cleanupContext); copyErr != nil {
		cleanupErr = errors.Join(cleanupErr, copyErr)
	}
	cleanupErr = errors.Join(cleanupErr, checkProcessGroupsStopped(processGroups))
	return cleanupErr
}

// processIdentity contains the process relationships needed to find descendant process groups.
type processIdentity struct {
	processID      int
	parentID       int
	processGroupID int
}

// discoverProcessGroups takes one process snapshot before cancellation can reparent descendants.
func discoverProcessGroups(ctx context.Context, rootProcessID int) ([]int, error) {
	command := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,pgid=")
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect pseudo-terminal process tree: %w", err)
	}

	identities := make([]processIdentity, 0)
	fields := strings.Fields(string(output))
	if len(fields)%3 != 0 {
		return nil, errors.New("inspect pseudo-terminal process tree: unexpected ps output")
	}
	for index := 0; index < len(fields); index += 3 {
		processID, processErr := strconv.Atoi(fields[index])
		parentID, parentErr := strconv.Atoi(fields[index+1])
		processGroupID, groupErr := strconv.Atoi(fields[index+2])
		if parseErr := errors.Join(processErr, parentErr, groupErr); parseErr != nil {
			return nil, fmt.Errorf("inspect pseudo-terminal process tree: %w", parseErr)
		}
		identities = append(identities, processIdentity{
			processID:      processID,
			parentID:       parentID,
			processGroupID: processGroupID,
		})
	}

	descendants := map[int]struct{}{rootProcessID: {}}
	for changed := true; changed; {
		changed = false
		for _, identity := range identities {
			if _, parentFound := descendants[identity.parentID]; !parentFound {
				continue
			}
			if _, found := descendants[identity.processID]; found {
				continue
			}
			descendants[identity.processID] = struct{}{}
			changed = true
		}
	}

	processGroups := make([]int, 0)
	for _, identity := range identities {
		if _, found := descendants[identity.processID]; !found {
			continue
		}
		if identity.processGroupID <= 0 || slices.Contains(processGroups, identity.processGroupID) {
			continue
		}
		processGroups = append(processGroups, identity.processGroupID)
	}
	return processGroups, nil
}

// terminateProcessGroups signals every recorded group once and preserves all signal errors.
func terminateProcessGroups(processGroups []int) error {
	var terminateErr error
	for _, processGroupID := range slices.Backward(processGroups) {
		if err := syscall.Kill(-processGroupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			terminateErr = errors.Join(
				terminateErr,
				fmt.Errorf("terminate process group %d: %w", processGroupID, err),
			)
		}
	}
	return terminateErr
}

// checkProcessGroupsStopped performs one final state check after both completion channels resolve.
func checkProcessGroupsStopped(processGroups []int) error {
	liveProcessGroups := make([]int, 0)
	var checkErr error
	for _, processGroupID := range processGroups {
		err := syscall.Kill(-processGroupID, 0)
		switch {
		case err == nil:
			liveProcessGroups = append(liveProcessGroups, processGroupID)
		case errors.Is(err, syscall.ESRCH):
		default:
			liveProcessGroups = append(liveProcessGroups, processGroupID)
			checkErr = errors.Join(checkErr, fmt.Errorf("check process group %d: %w", processGroupID, err))
		}
	}
	if len(liveProcessGroups) > 0 {
		checkErr = errors.Join(checkErr, fmt.Errorf("process groups still running: %v", liveProcessGroups))
	}
	return checkErr
}
