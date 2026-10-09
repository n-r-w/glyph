//go:build integration

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	manager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// invocationTransportFailureText includes peer-owned text that matches the Host classification.
const invocationTransportFailureText = "complete invocation transport failure: " +
	"extension runtime unavailable; original peer cause Ω"

// TestManagerFailureReturnsBeforeRuntimeShutdown checks worker return and owner cleanup through a real process.
func TestManagerFailureReturnsBeforeRuntimeShutdown(t *testing.T) {
	t.Parallel()

	// Arrange failure routes with and without process observation.
	for _, monitored := range []bool{false, true} {
		t.Run(fmt.Sprintf("monitored=%t", monitored), func(t *testing.T) {
			t.Parallel()
			for _, invocation := range []string{"tool", "handler", "lifecycle", "compaction"} {
				t.Run(invocation, func(t *testing.T) {
					t.Parallel()
					modes := []string{"shutdown-host-work-loss", "shutdown-host-work-cancel-handler"}
					if invocation == "tool" {
						modes = []string{"shutdown-host-work", "shutdown-host-work-loss", "shutdown-host-work-cancel"}
					}
					for _, mode := range modes {
						t.Run(mode, func(t *testing.T) {
							t.Parallel()
							// Act and assert failure settlement through the production runtime and manager.
							assertManagerFailureShutdown(t, mode, invocation, monitored, "close")
						})
					}
				})
			}
		})
	}
}

// TestManagerRemovalJoinsFailedRuntimeGeneration checks replacement and rejection of a failed process owner.
func TestManagerRemovalJoinsFailedRuntimeGeneration(t *testing.T) {
	t.Parallel()
	// Arrange removal paths for an observed failed runtime.
	for _, completion := range []string{"replacement", "rejection"} {
		t.Run(completion, func(t *testing.T) {
			t.Parallel()
			// Act and assert cleanup before ownership moves to another generation.
			assertManagerFailureShutdown(t, "shutdown-host-work", "tool", true, completion)
		})
	}
}

// writeRuntimeExecutable prepares a fixture executable without exposing its writable descriptor to concurrent forks.
func writeRuntimeExecutable(path, script string) error {
	// A fork can temporarily inherit even a close-on-exec descriptor and prevent execution of that file.
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	return os.WriteFile(path, []byte(script), 0o755)
}

// assertManagerFailureShutdown checks admission, worker return, and owner cleanup through a real process.
func assertManagerFailureShutdown(t *testing.T, mode, invocation string, monitored bool, completion string) {
	// toolInvocation selects tool failure classification assertions.
	const toolInvocation = "tool"

	t.Helper()

	// Arrange a real runtime with admitted Host work whose release needs its invocation to return.
	controller := gomock.NewController(t)
	host := NewMockHostService(controller)
	operation := NewMockHostOperation(controller)
	reporter := manager.NewMockFailureReporter(controller)
	reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	factory := NewFactory()
	service := manager.New(catalog.New(), factory, reporter)
	sourceErr := errors.New("complete nested Host operation source cause")
	releaseStarted := make(chan struct{})
	hostStarted := make(chan struct{})
	allowRelease := make(chan struct{})
	release := sync.OnceFunc(func() { close(allowRelease) })
	t.Cleanup(func() { release(); _ = service.Close() })
	factory.BindHostServiceFactory(func(extensionID, instanceID string) extensionsdk.HostService {
		host.EXPECT().
			Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
			MaxTimes(1).
			DoAndReturn(func(
				ctx context.Context, _ string, _ *extensionpb.ExtensionRequest,
			) (extensionsdk.HostOperation, error) {
				finish, err := service.BeginContextOperation(ctx, extensionID, instanceID)
				if err != nil {
					return nil, err
				}
				operation.EXPECT().Release().Do(func() {
					close(releaseStarted)
					<-allowRelease
					finish()
				})
				return operation, nil
			})
		return host
	})
	operation.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
			close(hostStarted)
			<-ctx.Done()
			return nil, sourceErr
		})
	directory := t.TempDir()
	path := filepath.Join(directory, "shutdown")
	pidPath := filepath.Join(t.TempDir(), "process-pid")
	script := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s' $$ > %q\n%s=%s exec %q -test.run=^TestRuntimeHelperProcess$\n",
		pidPath,
		runtimeHelperEnvironment,
		mode,
		os.Args[0],
	)
	require.NoError(t, writeRuntimeExecutable(path, script))
	pending, err := service.LoadPending(t.Context(), startup.Directory{Path: directory, Explicit: true})
	require.NoError(t, err)
	require.Empty(t, pending.Issues)
	require.Len(t, pending.Registrations, 1)
	service.Accept([]startup.AcceptedRegistration{{ID: "shutdown", Path: path, Tools: nil, Handlers: nil}})
	if monitored {
		service.Activate(t.Context())
	}
	instanceID, available := service.ContextRuntime("shutdown")
	require.True(t, available)
	binding := runtimeTestContext()
	binding.ExtensionID = "shutdown"
	binding.RuntimeInstanceID = instanceID

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	progress := func(tool.Progress) error {
		if mode == "shutdown-host-work-cancel" {
			cancel()
		}
		return nil
	}

	// Act through public manager invocation while the nested Host release remains pending.
	finished := make(chan error, 1)
	go func() {
		var invokeErr error
		switch invocation {
		case "tool":
			_, invokeErr = service.ExecuteTool(ctx, "shutdown", "read", []byte(`{}`), progress, binding)
		case "handler":
			_, invokeErr = service.HandleHandler(ctx, "shutdown", "observer", sessiontree.HandlerRequest{
				Context: binding, Request: mo.None[sessiontree.RequestHandlerInvocation](),
				Result: mo.None[sessiontree.ResultHandlerInvocation](),
				Observer: mo.Some(sessiontree.TreeObserverInvocation{
					SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
					NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
					CreatedSummary: mo.None[session.Entry](),
				}),
			})
		case "lifecycle":
			_, invokeErr = service.ObserveLifecycle(ctx, "shutdown", "observer", binding, lifecycle.Event{
				Agent: agent.Event{}, Settled: true, Selection: modelselection.SelectionChange{},
				SelectionEvent: false, ReasoningSelection: false,
			})
		case "compaction":
			_, invokeErr = service.GenerateCompaction(ctx, contextcompaction.Handler{
				ExtensionID: "shutdown", RuntimeID: instanceID, HandlerID: "generator",
			}, binding, contextcompaction.RequestInvocation{})
		}
		finished <- invokeErr
	}()
	if mode == "shutdown-host-work-cancel-handler" {
		select {
		case <-hostStarted:
			cancel()
		case <-time.After(processOperationTimeout):
			release()
			t.Fatal("nested Host work did not start")
		}
	}
	select {
	case <-releaseStarted:
	case <-time.After(processOperationTimeout):
		release()
		t.Fatal("nested Host release did not start")
	}
	var executeErr error
	select {
	case executeErr = <-finished:
	case <-time.After(time.Second):
		assert.Fail(t, "invocation waited for runtime shutdown that needs its Host worker")
	}

	// Assert admission is sealed, manager close waits, and acquired source causes survive cleanup.
	pidData, readErr := os.ReadFile(pidPath)
	require.NoError(t, readErr)
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
	require.NoError(t, parseErr)
	process, findErr := os.FindProcess(pid)
	require.NoError(t, findErr)
	assert.NoError(t, process.Signal(syscall.Signal(0)), "process shutdown preceded joined transport cleanup")
	assert.False(t, service.ToolRuntimeAvailable("shutdown"))
	closed := make(chan error, 1)
	go func() {
		switch completion {
		case "close":
			closed <- service.Close()
		case "rejection":
			service.RejectPending([]string{"shutdown"})
			closed <- nil
		case "replacement":
			script := fmt.Sprintf(
				"#!/bin/sh\n%s=wait-release exec %q -test.run=^TestRuntimeHelperProcess$\n",
				runtimeHelperEnvironment,
				os.Args[0],
			)
			if writeErr := writeRuntimeExecutable(path, script); writeErr != nil {
				closed <- writeErr
				return
			}
			_, loadErr := service.LoadPending(t.Context(), startup.Directory{Path: directory, Explicit: true})
			closed <- loadErr
		}
	}()
	select {
	case <-closed:
		assert.Fail(t, "runtime owner returned before nested Host release")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	if executeErr == nil {
		executeErr = <-finished
	}
	require.ErrorIs(t, executeErr, manager.ErrExtensionUnavailable)
	if invocation == toolInvocation {
		// expectedMarkers counts the Host classification and any matching peer-owned text.
		expectedMarkers := 1
		if mode == "shutdown-host-work-loss" {
			require.ErrorContains(t, executeErr, invocationTransportFailureText)
			expectedMarkers++
		}
		assert.Equal(t, expectedMarkers, strings.Count(executeErr.Error(), manager.ErrExtensionUnavailable.Error()))
	}
	if mode == "shutdown-host-work" {
		require.ErrorContains(t, executeErr, "result contents are empty")
	} else {
		require.ErrorContains(t, executeErr, "complete invocation transport failure")
	}
	if mode == "shutdown-host-work-cancel" || mode == "shutdown-host-work-cancel-handler" {
		require.ErrorIs(t, executeErr, context.Canceled)
	}
	select {
	case completionErr := <-closed:
		if completion == "close" {
			require.ErrorIs(t, completionErr, sourceErr)
			require.ErrorContains(t, completionErr, sourceErr.Error())
		} else {
			require.NoError(t, completionErr)
		}
		require.Error(t, process.Signal(syscall.Signal(0)), "runtime owner did not join the old process")
		if completion == "replacement" {
			service.Accept([]startup.AcceptedRegistration{{ID: "shutdown", Path: path, Tools: nil, Handlers: nil}})
			replacementID, replacementAvailable := service.ContextRuntime("shutdown")
			require.True(t, replacementAvailable)
			require.NotEqual(t, instanceID, replacementID)
			_, staleErr := service.BeginContextOperation(t.Context(), "shutdown", instanceID)
			require.ErrorContains(t, staleErr, "stale or unavailable")
			finish, reserveErr := service.BeginContextOperation(t.Context(), "shutdown", replacementID)
			require.NoError(t, reserveErr)
			finish()
		}
		closeErr := service.Close()
		require.ErrorIs(t, closeErr, sourceErr)
		require.ErrorContains(t, closeErr, sourceErr.Error())
		require.ErrorIs(t, service.Close(), sourceErr)
	case <-time.After(processOperationTimeout):
		t.Fatal("manager Close did not join runtime shutdown")
	}
}
