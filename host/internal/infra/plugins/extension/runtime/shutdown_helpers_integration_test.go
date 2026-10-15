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

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	manager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// shutdownExtensionID identifies the executable and runtime generation used in shutdown scenarios.
	shutdownExtensionID = "shutdown"
	// shutdownToolInvocation selects the public tool invocation path.
	shutdownToolInvocation = "tool"
	// shutdownHandlerInvocation selects the public session-tree handler path.
	shutdownHandlerInvocation = "handler"
	// shutdownLifecycleInvocation selects the public lifecycle observer path.
	shutdownLifecycleInvocation = "lifecycle"
	// shutdownCompactionInvocation selects the public compaction generator path.
	shutdownCompactionInvocation = "compaction"
	// shutdownCloseCompletion selects manager Close as the runtime owner join.
	shutdownCloseCompletion = "close"
	// shutdownRejectionCompletion selects rejection as the runtime owner join.
	shutdownRejectionCompletion = "rejection"
	// shutdownReplacementCompletion selects replacement as the runtime owner join.
	shutdownReplacementCompletion = "replacement"
	// shutdownScriptTemplate starts a protocol fixture with the selected mode.
	shutdownScriptTemplate = "#!/bin/sh\n%s=%s exec %q -test.run=^TestRuntimeHelperProcess$\n"
)

// managerShutdownFixture holds the runtime owner and the blocked Host release for one scenario.
type managerShutdownFixture struct {
	// service owns the runtime generation under test.
	service *manager.Service
	// sourceErr is the acquired nested Host failure.
	sourceErr error
	// releaseStarted signals entry into transport cleanup.
	releaseStarted <-chan struct{}
	// hostStarted signals execution of admitted Host work.
	hostStarted <-chan struct{}
	// release allows the Host worker to finish cleanup.
	release func()
	// directory holds the path used for fixture discovery.
	directory string
	// path identifies the executable used for replacement.
	path string
	// pidPath identifies the file containing the original process identity.
	pidPath string
	// instanceID identifies the original runtime generation.
	instanceID string
}

// assertManagerFailureShutdown checks worker settlement before the runtime owner joins cleanup.
func assertManagerFailureShutdown(t *testing.T, mode, invocation string, monitored bool, completion string) {
	t.Helper()
	fixture := prepareManagerShutdown(t, mode, monitored)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished, executeErr := assertManagerShutdownInvocation(t, ctx, cancel, fixture, mode, invocation)

	// Assert admission and process lifetime before owner completion.
	pidData, readErr := os.ReadFile(fixture.pidPath)
	require.NoError(t, readErr)
	// pid is the original child identity recorded before protocol startup.
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
	require.NoError(t, parseErr)
	process, findErr := os.FindProcess(pid)
	require.NoError(t, findErr)
	assert.NoError(t, process.Signal(syscall.Signal(0)), "process shutdown preceded joined transport cleanup")
	assert.False(t, fixture.service.ToolRuntimeAvailable(shutdownExtensionID))
	closed := startManagerShutdownCompletion(t, fixture, completion)
	fixture.release()
	if executeErr == nil {
		executeErr = <-finished
	}
	assertManagerShutdownFailure(t, executeErr, mode, invocation)
	assertManagerShutdownCompletion(t, fixture, process, closed, completion)
}

// prepareManagerShutdown starts a real runtime with a blocked nested Host release.
func prepareManagerShutdown(t *testing.T, mode string, monitored bool) managerShutdownFixture {
	const shutdownPIDScriptTemplate = "#!/bin/sh\nprintf '%%s' $$ > %q\n" +
		"%s=%s exec %q -test.run=^TestRuntimeHelperProcess$\n"

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
	// releaseStarted signals that nested Host cleanup has begun.
	releaseStarted := make(chan struct{})
	// hostStarted signals admission and execution before caller cancellation.
	hostStarted := make(chan struct{})
	// allowRelease is the parent-controlled gate for nested Host cleanup.
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
	path := filepath.Join(directory, shutdownExtensionID)
	pidPath := filepath.Join(t.TempDir(), "process-pid")
	script := fmt.Sprintf(
		shutdownPIDScriptTemplate,
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
	service.Accept([]startup.AcceptedRegistration{{ID: shutdownExtensionID, Path: path, Tools: nil, Handlers: nil}})
	if monitored {
		service.Activate(t.Context())
	}
	instanceID, available := service.ContextRuntime(shutdownExtensionID)
	require.True(t, available)
	return managerShutdownFixture{
		service: service, sourceErr: sourceErr, releaseStarted: releaseStarted,
		hostStarted: hostStarted, release: release, directory: directory,
		path: path, pidPath: pidPath, instanceID: instanceID,
	}
}

// assertManagerShutdownInvocation checks prompt public invocation return during blocked cleanup.
func assertManagerShutdownInvocation(
	t *testing.T, ctx context.Context, cancel context.CancelFunc,
	fixture managerShutdownFixture, mode, invocation string,
) (<-chan error, error) {
	const shutdownGeneratorID = "generator"

	t.Helper()
	// Arrange the binding and caller cancellation for the selected invocation.
	binding := runtimeTestContext()
	binding.ExtensionID = shutdownExtensionID
	binding.RuntimeInstanceID = fixture.instanceID
	// progress triggers caller cancellation after tool progress reaches the parent.
	progress := func(tool.Progress) error {
		if mode == shutdownHostWorkCancelMode {
			cancel()
		}
		return nil
	}

	// Act through public manager invocation while the nested Host release remains pending.
	// finished carries the worker outcome independently of owner cleanup.
	finished := make(chan error, 1)
	go func() {
		var invokeErr error
		switch invocation {
		case shutdownToolInvocation:
			_, invokeErr = fixture.service.ExecuteTool(
				ctx,
				shutdownExtensionID,
				fixtureToolName,
				[]byte(`{}`),
				progress,
				binding,
			)
		case shutdownHandlerInvocation:
			_, invokeErr = fixture.service.HandleHandler(
				ctx,
				shutdownExtensionID,
				fixtureObserverID,
				sessiontree.HandlerRequest{
					Context: binding, Request: mo.None[sessiontree.RequestHandlerInvocation](),
					Result: mo.None[sessiontree.ResultHandlerInvocation](),
					Observer: mo.Some(sessiontree.TreeObserverInvocation{
						SessionID:               fixtureSessionID,
						TargetEntryID:           fixtureTargetEntryID,
						PrecedingActiveLeafID:   mo.None[string](),
						NavigationDestinationID: mo.None[string](),
						CommittedActiveLeafID:   mo.None[string](),
						CreatedSummary:          mo.None[session.Entry](),
					}),
				},
			)
		case shutdownLifecycleInvocation:
			_, invokeErr = fixture.service.ObserveLifecycle(
				ctx,
				shutdownExtensionID,
				fixtureObserverID,
				binding,
				lifecycle.Event{
					Agent: agent.Event{}, Settled: true, Selection: modelselection.SelectionChange{},
					SelectionEvent: false, ReasoningSelection: false,
				},
			)
		case shutdownCompactionInvocation:
			_, invokeErr = fixture.service.InvokeCompactionGenerator(ctx, extensioncontext.Participant{
				ExtensionID: shutdownExtensionID, RuntimeID: fixture.instanceID, HandlerID: shutdownGeneratorID,
			}, binding, extensioncontext.RequestInvocation{})
		}
		finished <- invokeErr
	}()
	if mode == shutdownHostWorkCancelHandlerMode {
		select {
		case <-fixture.hostStarted:
			cancel()
		case <-time.After(processOperationTimeout):
			t.Fatal("nested Host work did not start")
		}
	}
	select {
	case <-fixture.releaseStarted:
	case <-time.After(processOperationTimeout):
		t.Fatal("nested Host release did not start")
	}
	var executeErr error
	select {
	case executeErr = <-finished:
	case <-time.After(time.Second):
		assert.Fail(t, "invocation waited for runtime shutdown that needs its Host worker")
	}

	return finished, executeErr
}

// startManagerShutdownCompletion checks that the selected owner join waits for Host release.
func startManagerShutdownCompletion(
	t *testing.T, fixture managerShutdownFixture, completion string,
) <-chan error {
	t.Helper()
	closed := make(chan error, 1)
	go func() {
		switch completion {
		case shutdownCloseCompletion:
			closed <- fixture.service.Close()
		case shutdownRejectionCompletion:
			fixture.service.RejectPending([]string{shutdownExtensionID})
			closed <- nil
		case shutdownReplacementCompletion:
			script := fmt.Sprintf(
				shutdownScriptTemplate,
				runtimeHelperEnvironment,
				runtimeWaitReleaseMode,
				os.Args[0],
			)
			if writeErr := writeRuntimeExecutable(fixture.path, script); writeErr != nil {
				closed <- writeErr
				return
			}
			_, loadErr := fixture.service.LoadPending(
				t.Context(),
				startup.Directory{Path: fixture.directory, Explicit: true},
			)
			closed <- loadErr
		}
	}()
	select {
	case <-closed:
		assert.Fail(t, "runtime owner returned before nested Host release")
	case <-time.After(50 * time.Millisecond):
	}
	return closed
}

// assertManagerShutdownFailure checks invocation cause identity, source text, and classification count.
func assertManagerShutdownFailure(t *testing.T, executeErr error, mode, invocation string) {
	t.Helper()
	require.ErrorIs(t, executeErr, manager.ErrExtensionUnavailable)
	if invocation == shutdownToolInvocation {
		// expectedMarkers counts the Host classification and any matching peer-owned text.
		expectedMarkers := 1
		if mode == shutdownHostWorkLossMode {
			require.ErrorContains(t, executeErr, invocationTransportFailureText)
			expectedMarkers++
		}
		assert.Equal(t, expectedMarkers, strings.Count(executeErr.Error(), manager.ErrExtensionUnavailable.Error()))
	}
	if mode == shutdownHostWorkMode {
		require.ErrorContains(t, executeErr, "result contents are empty")
	} else {
		require.ErrorContains(t, executeErr, "complete invocation transport failure")
	}
	if mode == shutdownHostWorkCancelMode || mode == shutdownHostWorkCancelHandlerMode {
		require.ErrorIs(t, executeErr, context.Canceled)
	}
}

// assertManagerShutdownCompletion checks process joining, generation identity, and retained cleanup causes.
func assertManagerShutdownCompletion(
	t *testing.T, fixture managerShutdownFixture, process *os.Process, closed <-chan error, completion string,
) {
	t.Helper()
	select {
	case completionErr := <-closed:
		if completion == shutdownCloseCompletion {
			require.ErrorIs(t, completionErr, fixture.sourceErr)
			require.ErrorContains(t, completionErr, fixture.sourceErr.Error())
		} else {
			require.NoError(t, completionErr)
		}
		require.Error(t, process.Signal(syscall.Signal(0)), "runtime owner did not join the old process")
		if completion == shutdownReplacementCompletion {
			fixture.service.Accept(
				[]startup.AcceptedRegistration{
					{ID: shutdownExtensionID, Path: fixture.path, Tools: nil, Handlers: nil},
				},
			)
			replacementID, replacementAvailable := fixture.service.ContextRuntime(shutdownExtensionID)
			require.True(t, replacementAvailable)
			require.NotEqual(t, fixture.instanceID, replacementID)
			_, staleErr := fixture.service.BeginContextOperation(t.Context(), shutdownExtensionID, fixture.instanceID)
			require.ErrorContains(t, staleErr, "stale or unavailable")
			finish, reserveErr := fixture.service.BeginContextOperation(t.Context(), shutdownExtensionID, replacementID)
			require.NoError(t, reserveErr)
			finish()
		}
		closeErr := fixture.service.Close()
		require.ErrorIs(t, closeErr, fixture.sourceErr)
		require.ErrorContains(t, closeErr, fixture.sourceErr.Error())
		require.ErrorIs(t, fixture.service.Close(), fixture.sourceErr)
	case <-time.After(processOperationTimeout):
		t.Fatal("manager Close did not join runtime shutdown")
	}
}
