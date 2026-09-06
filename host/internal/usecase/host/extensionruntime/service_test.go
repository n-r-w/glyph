//go:build !integration

package extensionruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestRuntimeUnavailableShutdownReleasesCommitLockBeforeTransportClose verifies shutdown cannot block stale commit attempts.
func TestRuntimeUnavailableShutdownReleasesCommitLockBeforeTransportClose(t *testing.T) {
	t.Parallel()

	// Arrange one failing admitted operation whose transport close waits for another Host operation.
	controller := gomock.NewController(t)
	process := NewMockExtensionRuntime(controller)
	closeStarted := make(chan struct{})
	allowClose := make(chan struct{})
	process.EXPECT().Close().Do(func() {
		close(closeStarted)
		<-allowClose
	})
	state := &runtimeState{
		runtime: process, instanceID: "runtime", available: true, activeExecutions: 1,
		exitPending: false, work: sync.WaitGroup{}, closeOnce: sync.Once{}, observed: false,
		monitorDone: make(chan struct{}), commit: sync.RWMutex{}, invalidated: make(chan struct{}),
		invalidateOnce: sync.Once{},
	}
	state.work.Add(1)
	service := &Service{
		catalog: nil, factory: nil,
		reportFailure: func(context.Context, extension.RuntimeFailure) error { return nil },
		mutex:         sync.RWMutex{}, runtimes: map[string]*runtimeState{"extension": state},
		monitoring: false, monitorContext: nil, closing: false,
	}
	finishDone := make(chan struct{})
	go func() {
		service.finishAndReport(
			t.Context(), operationOwner{pluginID: "extension", state: state}, ErrExtensionUnavailable,
		)
		close(finishDone)
	}()
	<-closeStarted

	// Act by checking the commit boundary while transport closure remains blocked.
	commitLockAvailable := state.commit.TryRLock()
	if !commitLockAvailable {
		close(allowClose)
		<-finishDone
		t.Fatal("runtime shutdown held the commit lock during transport closure")
	}
	state.commit.RUnlock()
	_, err := service.BeginContextCommit("extension", "runtime")

	// Assert invalidated work fails immediately and shutdown can finish after transport closure.
	failure, found := errors.AsType[extensioncontroller.ContextFailure](err)
	require.True(t, found)
	assert.Equal(t, "STALE_CONTEXT", failure.ContextCode())
	close(allowClose)
	<-finishDone
}

// TestRuntimeReplacementCannotOvertakeAdmittedAppendCommit verifies invalidation waits for the owning mutation.
func TestRuntimeReplacementCannotOvertakeAdmittedAppendCommit(t *testing.T) {
	t.Parallel()

	// Arrange one accepted runtime, issued context, and an admitted append blocked before persistence.
	controller := gomock.NewController(t)
	process := NewMockExtensionRuntime(controller)
	transportClosed := make(chan struct{})
	process.EXPECT().Close().Do(func() { close(transportClosed) })
	state := &runtimeState{
		runtime: process, instanceID: "runtime", available: true, activeExecutions: 0,
		exitPending: false, work: sync.WaitGroup{}, closeOnce: sync.Once{}, observed: false,
		monitorDone: make(chan struct{}), commit: sync.RWMutex{}, invalidated: make(chan struct{}),
		invalidateOnce: sync.Once{},
	}
	runtimes := &Service{
		catalog: nil, factory: nil, reportFailure: nil, mutex: sync.RWMutex{},
		runtimes: map[string]*runtimeState{"extension": state}, monitoring: false,
		monitorContext: nil, closing: false,
	}
	sessions := extensioncontext.NewMockSessionState(controller)
	identity := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	commitStarted := make(chan struct{})
	allowPersistence := make(chan struct{})
	var persisted atomic.Bool
	stored := session.Entry{
		ID:            "entry",
		ParentID:      mo.None[string](),
		CreatedAt:     time.Unix(1, 0).UTC(),
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension: mo.Some(
			session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "state", Data: []byte(`{}`)},
		),
		BranchSummary: mo.None[session.BranchSummaryEntry](), ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
	sessions.EXPECT().AppendExtension(gomock.Any(), identity, stored.Extension.MustGet(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ extensioncontext.SessionIdentity,
			_ session.ExtensionEnvelope,
			guard extensioncontext.ContextCommitGuard,
		) (session.Entry, error) {
			release, err := guard()
			if err != nil {
				return session.Entry{}, err
			}
			defer release()
			close(commitStarted)
			<-allowPersistence
			persisted.Store(true)
			return stored, nil
		},
	)
	contexts := extensioncontext.New(runtimes, sessions)
	issued, err := contexts.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{
		ID: issued.ID, RuntimeInstanceID: issued.RuntimeInstanceID, SessionID: issued.SessionID,
	}
	releaseOperation, err := runtimes.BeginContextOperation(t.Context(), "extension", "runtime")
	require.NoError(t, err)
	appendDone := make(chan error, 1)
	go func() {
		_, appendErr := contexts.AppendExtension(
			t.Context(), "extension", "runtime", reference, "state", []byte(`{}`),
		)
		appendDone <- appendErr
	}()
	<-commitStarted
	replacementDone := make(chan struct{})
	go func() {
		runtimes.replaceInstance("extension")
		close(replacementDone)
	}()
	<-state.invalidated

	// Act while persistence remains blocked after final runtime validation.
	assert.Never(t, func() bool {
		select {
		case <-transportClosed:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, time.Millisecond)
	close(allowPersistence)

	// Assert persistence finishes before replacement closes transport and joins admitted work.
	require.NoError(t, <-appendDone)
	require.True(t, persisted.Load())
	releaseOperation()
	<-replacementDone
	select {
	case <-transportClosed:
	default:
		t.Fatal("runtime replacement did not close transport")
	}
}

// TestRuntimeReplacementBeforeFinalAppendValidationRejectsCommit verifies admitted stale work cannot persist.
func TestRuntimeReplacementBeforeFinalAppendValidationRejectsCommit(t *testing.T) {
	t.Parallel()

	// Arrange one admitted append that pauses before it acquires the final runtime commit guard.
	controller := gomock.NewController(t)
	process := NewMockExtensionRuntime(controller)
	process.EXPECT().Close()
	state := &runtimeState{
		runtime: process, instanceID: "runtime", available: true, activeExecutions: 0,
		exitPending: false, work: sync.WaitGroup{}, closeOnce: sync.Once{}, observed: false,
		monitorDone: make(chan struct{}), commit: sync.RWMutex{}, invalidated: make(chan struct{}),
		invalidateOnce: sync.Once{},
	}
	runtimes := &Service{
		catalog: nil, factory: nil, reportFailure: nil, mutex: sync.RWMutex{},
		runtimes: map[string]*runtimeState{"extension": state}, monitoring: false,
		monitorContext: nil, closing: false,
	}
	sessions := extensioncontext.NewMockSessionState(controller)
	identity := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	appendAdmitted := make(chan struct{})
	var persisted atomic.Bool
	sessions.EXPECT().AppendExtension(gomock.Any(), identity, gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ extensioncontext.SessionIdentity,
			_ session.ExtensionEnvelope,
			guard extensioncontext.ContextCommitGuard,
		) (session.Entry, error) {
			close(appendAdmitted)
			<-state.invalidated
			release, err := guard()
			if err != nil {
				return session.Entry{}, err
			}
			defer release()
			persisted.Store(true)
			return session.Entry{}, nil
		},
	)
	contexts := extensioncontext.New(runtimes, sessions)
	issued, err := contexts.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{
		ID: issued.ID, RuntimeInstanceID: issued.RuntimeInstanceID, SessionID: issued.SessionID,
	}
	releaseOperation, err := runtimes.BeginContextOperation(t.Context(), "extension", "runtime")
	require.NoError(t, err)
	appendDone := make(chan error, 1)
	go func() {
		_, appendErr := contexts.AppendExtension(
			t.Context(), "extension", "runtime", reference, "state", []byte(`{}`),
		)
		appendDone <- appendErr
	}()
	<-appendAdmitted
	replacementDone := make(chan struct{})
	go func() {
		runtimes.replaceInstance("extension")
		close(replacementDone)
	}()

	// Act after the concrete runtime starts invalidating the admitted operation's instance.
	err = <-appendDone
	releaseOperation()
	<-replacementDone

	// Assert final validation returns STALE_CONTEXT and the owning mutation does not persist.
	failure, found := errors.AsType[extensioncontroller.ContextFailure](err)
	require.True(t, found)
	assert.Equal(t, "STALE_CONTEXT", failure.ContextCode())
	assert.Contains(t, err.Error(), "stale or unavailable")
	assert.False(t, persisted.Load())
}

// TestRuntimeReplacementBeforeMessageCommitPreservesStaleCategory verifies final guard classification for messages.
func TestRuntimeReplacementBeforeMessageCommitPreservesStaleCategory(t *testing.T) {
	t.Parallel()

	// Arrange one admitted message append that pauses before the final runtime commit guard.
	controller := gomock.NewController(t)
	process := NewMockExtensionRuntime(controller)
	process.EXPECT().Close()
	state := &runtimeState{
		runtime: process, instanceID: "runtime", available: true, activeExecutions: 0,
		exitPending: false, work: sync.WaitGroup{}, closeOnce: sync.Once{}, observed: false,
		monitorDone: make(chan struct{}), commit: sync.RWMutex{}, invalidated: make(chan struct{}),
		invalidateOnce: sync.Once{},
	}
	runtimes := &Service{
		catalog: nil, factory: nil, reportFailure: nil, mutex: sync.RWMutex{},
		runtimes: map[string]*runtimeState{"extension": state}, monitoring: false,
		monitorContext: nil, closing: false,
	}
	sessions := extensioncontext.NewMockSessionState(controller)
	identity := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	appendAdmitted := make(chan struct{})
	var persisted atomic.Bool
	sessions.EXPECT().AppendExtensionMessage(
		gomock.Any(), identity, gomock.Any(), gomock.Any(), gomock.Any(),
	).DoAndReturn(func(
		_ context.Context,
		_ extensioncontext.SessionIdentity,
		_ session.ExtensionMessage,
		guard extensioncontext.ContextCommitGuard,
		_ func(session.Entry) (func(context.Context) error, error),
	) (session.Entry, error) {
		close(appendAdmitted)
		<-state.invalidated
		release, err := guard()
		if err != nil {
			return session.Entry{}, fmt.Errorf("validate message commit: %w", err)
		}
		defer release()
		persisted.Store(true)
		return session.Entry{}, nil
	})
	contexts := extensioncontext.New(runtimes, sessions)
	issued, err := contexts.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{
		ID: issued.ID, RuntimeInstanceID: issued.RuntimeInstanceID, SessionID: issued.SessionID,
	}
	releaseOperation, err := runtimes.BeginContextOperation(t.Context(), "extension", "runtime")
	require.NoError(t, err)
	appendDone := make(chan error, 1)
	go func() {
		_, appendErr := contexts.AppendExtensionMessage(
			t.Context(), "extension", "runtime", reference,
			"note", "exact text", session.ClientVisibilityVisible,
		)
		appendDone <- appendErr
	}()
	<-appendAdmitted
	replacementDone := make(chan struct{})
	go func() {
		runtimes.replaceInstance("extension")
		close(replacementDone)
	}()

	// Act after the runtime starts invalidating the admitted operation's instance.
	err = <-appendDone
	releaseOperation()
	<-replacementDone

	// Assert the closed category and complete wrapped cause survive without persistence.
	failure, found := errors.AsType[extensioncontroller.ContextFailure](err)
	require.True(t, found)
	assert.Equal(t, "STALE_CONTEXT", failure.ContextCode())
	assert.Contains(t, err.Error(), "validate message commit")
	assert.Contains(t, err.Error(), "stale or unavailable")
	assert.False(t, persisted.Load())
}

// TestServiceLoadsPendingAndActivatesAcceptedRuntime verifies pending runtimes remain unavailable until acceptance.
func TestServiceLoadsPendingAndActivatesAcceptedRuntime(t *testing.T) {
	t.Parallel()
	// Arrange test dependencies.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().
		Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).
		Return(Discovery{Candidates: []Candidate{{
			InstanceID: "",
			ID:         "tools", Path: "/tools",
		}}, Issues: nil}, nil)
	var startedInstance string
	factory.EXPECT().
		Start(t.Context(), gomock.Any()).
		DoAndReturn(func(_ context.Context, candidate Candidate) (ExtensionRuntime, error) {
			assert.Equal(t, "tools", candidate.ID)
			assert.Equal(t, "/tools", candidate.Path)
			assert.NotEmpty(t, candidate.InstanceID)
			startedInstance = candidate.InstanceID
			return runtime, nil
		})
	runtime.EXPECT().
		Register(t.Context()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	service := New(catalog, factory, discardRuntimeFailure)
	// Act load the runtime as pending, then accept it.
	pending, err := service.LoadPending(t.Context(), startup.Directory{Path: "/plugins", Explicit: true})
	before := service.ToolRuntimeAvailable("tools")
	service.Accept([]startup.AcceptedRegistration{{ID: "tools", Path: "/tools", Tools: nil, Handlers: nil}})
	// Assert availability changes only on acceptance.
	require.NoError(t, err)
	require.Len(t, pending.Registrations, 1)
	assert.False(t, before)
	assert.True(t, service.ToolRuntimeAvailable("tools"))
	instance, available := service.ContextRuntime("tools")
	assert.True(t, available)
	assert.Equal(t, startedInstance, instance)
	release, err := service.BeginContextOperation(t.Context(), "tools", instance)
	require.NoError(t, err)
	assert.Equal(t, 1, service.runtimes["tools"].activeExecutions)
	release()
	assert.Zero(t, service.runtimes["tools"].activeExecutions)
	_, err = service.BeginContextOperation(t.Context(), "tools", "old-runtime")
	require.Error(t, err)
	assert.ErrorContains(t, err, "old-runtime")
	runtime.EXPECT().Close()
	service.Close()
}

// TestRuntimeReplacementInvalidatesInstance verifies replacement joins the old process and never accepts its identity
// again.
func TestRuntimeReplacementInvalidatesInstance(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange: discover the same extension twice with separate process instances.
		controller := gomock.NewController(t)
		catalog := NewMockCatalog(controller)
		factory := NewMockRuntimeFactory(controller)
		first := NewMockExtensionRuntime(controller)
		second := NewMockExtensionRuntime(controller)
		firstDone := make(chan struct{})
		secondDone := make(chan struct{})
		first.EXPECT().Done().Return(firstDone).AnyTimes()
		second.EXPECT().Done().Return(secondDone).AnyTimes()
		catalog.EXPECT().
			Discover(gomock.Any(), gomock.Any()).
			Return(Discovery{Candidates: []Candidate{{ID: "extension", Path: "/extension", InstanceID: ""}}, Issues: nil}, nil).
			Times(2)
		gomock.InOrder(
			factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(first, nil),
			first.EXPECT().Register(gomock.Any()).Return(startup.PendingRegistration{}, nil),
			first.EXPECT().Close().Do(func() { close(firstDone) }),
			factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(second, nil),
			second.EXPECT().Register(gomock.Any()).Return(startup.PendingRegistration{}, nil),
		)
		service := New(catalog, factory, discardRuntimeFailure)
		accepted := []startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: nil, Handlers: nil}}
		_, err := service.LoadPending(t.Context(), startup.Directory{})
		require.NoError(t, err)
		service.Accept(accepted)
		old, available := service.ContextRuntime("extension")
		require.True(t, available)
		service.Activate(t.Context())

		// Act: replace and accept the process under the same extension identifier.
		_, err = service.LoadPending(t.Context(), startup.Directory{})
		require.NoError(t, err)
		service.Accept(accepted)
		current, available := service.ContextRuntime("extension")

		// Assert: only the replacement identity can admit extension-initiated work.
		require.True(t, available)
		assert.NotEqual(t, old, current)
		called := false
		second.EXPECT().
			Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(context.Context, string, []byte, tool.ProgressHandler, extension.Context) (tool.Result, error) {
				called = true
				return tool.Result{}, nil
			}).
			AnyTimes()
		_, staleErr := service.ExecuteTool(
			t.Context(),
			"extension",
			"read",
			nil,
			nil,
			extension.Context{
				ID:                "binding",
				ExtensionID:       "extension",
				RuntimeInstanceID: old,
				SessionID:         "session",
				WorkingDirectory:  "/project",
			},
		)
		assert.Error(t, staleErr)
		assert.False(t, called)
		_, err = service.BeginContextOperation(t.Context(), "extension", old)
		require.Error(t, err)
		release, err := service.BeginContextOperation(t.Context(), "extension", current)
		require.NoError(t, err)
		release()
		second.EXPECT().Close()
		close(secondDone)
		synctest.Wait()
		_, available = service.ContextRuntime("extension")
		assert.False(t, available, "replacement runtime exit was not monitored")
		service.Close()
	})
}

// TestServiceRejectPendingClosesWithoutFailure verifies startup rejection does not report runtime loss.
func TestServiceRejectPendingClosesWithoutFailure(t *testing.T) {
	t.Parallel()
	// Arrange one pending runtime and a recording failure sink.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	failures := make([]extension.RuntimeFailure, 0)
	catalog.EXPECT().
		Discover(gomock.Any(), gomock.Any()).
		Return(Discovery{Candidates: []Candidate{{
			InstanceID: "",
			ID:         "bad", Path: "/bad",
		}}, Issues: nil}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(gomock.Any()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().Close()
	service := New(catalog, factory, func(_ context.Context, failure extension.RuntimeFailure) error {
		failures = append(failures, failure)
		return nil
	})
	_, err := service.LoadPending(t.Context(), startup.Directory{})
	require.NoError(t, err)
	// Act reject the pending process.
	service.RejectPending([]string{"bad"})
	// Assert the runtime closes without becoming available or reporting failure.
	assert.False(t, service.ToolRuntimeAvailable("bad"))
	assert.Empty(t, failures)
}

// TestServiceExecuteToolPreservesUnavailableCause verifies active accounting and runtime failure reporting.
func TestServiceExecuteToolPreservesUnavailableCause(t *testing.T) {
	t.Parallel()
	// Arrange one accepted runtime that fails during execution.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	failures := make([]extension.RuntimeFailure, 0)
	catalog.EXPECT().
		Discover(gomock.Any(), gomock.Any()).
		Return(Discovery{Candidates: []Candidate{{
			InstanceID: "",
			ID:         "tools", Path: "/tools",
		}}, Issues: nil}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(gomock.Any()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().
		Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any(), gomock.Any()).
		Return(tool.Result{}, fmt.Errorf("process crashed: %w", ErrExtensionUnavailable))
	runtime.EXPECT().Close()
	service := New(catalog, factory, func(_ context.Context, failure extension.RuntimeFailure) error {
		failures = append(failures, failure)
		return nil
	})
	_, err := service.LoadPending(t.Context(), startup.Directory{})
	require.NoError(t, err)
	service.Accept([]startup.AcceptedRegistration{{ID: "tools", Path: "/tools", Tools: nil, Handlers: nil}})
	// Act invoke the unavailable runtime.
	_, executeErr := service.ExecuteTool(
		t.Context(),
		"tools",
		"read",
		[]byte(`{}`),
		func(tool.Progress) error { return nil }, runtimeBindingForTest(service, "tools"),
	)
	// Assert the complete cause is preserved and the runtime is disabled once.
	require.ErrorIs(t, executeErr, ErrExtensionUnavailable)
	require.ErrorContains(t, executeErr, "process crashed")
	assert.False(t, service.ToolRuntimeAvailable("tools"))
	assert.Equal(
		t,
		[]extension.RuntimeFailure{{PluginID: "tools", Condition: extension.RuntimeUnavailableProcessExited}},
		failures,
	)
}

// TestServiceReportsIdleRuntimeExit verifies monitoring disables and reports an accepted runtime once.
func TestServiceReportsIdleRuntimeExit(t *testing.T) {
	t.Parallel()
	// Arrange one accepted runtime and activate its process monitor.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	closed := make(chan struct{})
	failures := make(chan extension.RuntimeFailure, 1)
	catalog.EXPECT().
		Discover(gomock.Any(), gomock.Any()).
		Return(Discovery{Candidates: []Candidate{{
			InstanceID: "",
			ID:         "tools", Path: "/tools",
		}}, Issues: nil}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(gomock.Any()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Close().Do(func() { close(closed) })
	service := New(
		catalog,
		factory,
		func(_ context.Context, failure extension.RuntimeFailure) error { failures <- failure; return nil },
	)
	_, err := service.LoadPending(t.Context(), startup.Directory{})
	require.NoError(t, err)
	service.Accept([]startup.AcceptedRegistration{{ID: "tools", Path: "/tools", Tools: nil, Handlers: nil}})
	service.Activate(t.Context())
	// Act publish the process exit.
	close(done)
	// Assert the runtime becomes unavailable and reports the classified failure.
	select {
	case failure := <-failures:
		assert.Equal(
			t,
			extension.RuntimeFailure{PluginID: "tools", Condition: extension.RuntimeUnavailableProcessExited},
			failure,
		)
	case <-time.After(time.Second):
		require.Fail(t, "runtime failure was not reported")
	}
	<-closed
	assert.False(t, service.ToolRuntimeAvailable("tools"))
}

// TestServiceReportsExitAfterActiveExecution verifies exit reporting waits for active accounting to settle.
func TestServiceReportsExitAfterActiveExecution(t *testing.T) {
	t.Parallel()
	// Arrange one active successful invocation when its process exits.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	closed := make(chan struct{})
	started := make(chan struct{})
	allowResult := make(chan struct{})
	failures := make(chan extension.RuntimeFailure, 1)
	catalog.EXPECT().
		Discover(gomock.Any(), gomock.Any()).
		Return(Discovery{Candidates: []Candidate{{
			InstanceID: "",
			ID:         "tools", Path: "/tools",
		}}, Issues: nil}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(gomock.Any()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().
		Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, []byte, tool.ProgressHandler, extension.Context) (tool.Result, error) {
			close(started)
			<-allowResult
			return tool.Result{Contents: tool.TextContents("done"), IsError: false}, nil
		})
	runtime.EXPECT().Close().Do(func() { close(closed) })
	service := New(
		catalog,
		factory,
		func(_ context.Context, failure extension.RuntimeFailure) error { failures <- failure; return nil },
	)
	_, err := service.LoadPending(t.Context(), startup.Directory{})
	require.NoError(t, err)
	service.Accept([]startup.AcceptedRegistration{{ID: "tools", Path: "/tools", Tools: nil, Handlers: nil}})
	service.Activate(t.Context())
	execution := make(chan error, 1)
	go func() {
		_, executeErr := service.ExecuteTool(
			t.Context(),
			"tools",
			"read",
			[]byte(`{}`),
			func(tool.Progress) error { return nil }, runtimeBindingForTest(service, "tools"),
		)
		execution <- executeErr
	}()
	<-started
	// Act publish exit while the invocation is active, then settle the invocation.
	close(done)
	close(allowResult)
	// Assert successful completion and one deferred runtime failure report.
	require.NoError(t, <-execution)
	select {
	case failure := <-failures:
		assert.Equal(
			t,
			extension.RuntimeFailure{PluginID: "tools", Condition: extension.RuntimeUnavailableProcessExited},
			failure,
		)
	case <-time.After(time.Second):
		require.Fail(t, "runtime failure was not reported")
	}
	<-closed
	assert.False(t, service.ToolRuntimeAvailable("tools"))
}

// TestServiceCloseJoinsExtensionInitiatedAccounting verifies shutdown owns context-operation reservations until
// release.
func TestServiceCloseJoinsExtensionInitiatedAccounting(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange: accept a runtime and retain one extension-initiated operation reservation.
		controller := gomock.NewController(t)
		catalog := NewMockCatalog(controller)
		factory := NewMockRuntimeFactory(controller)
		runtime := NewMockExtensionRuntime(controller)
		catalog.EXPECT().
			Discover(gomock.Any(), gomock.Any()).
			Return(Discovery{Candidates: []Candidate{{ID: "extension", Path: "/extension", InstanceID: ""}}, Issues: nil}, nil)
		factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
		runtime.EXPECT().Register(gomock.Any()).Return(startup.PendingRegistration{}, nil)
		runtime.EXPECT().Close()
		service := New(catalog, factory, discardRuntimeFailure)
		_, err := service.LoadPending(t.Context(), startup.Directory{})
		require.NoError(t, err)
		service.Accept([]startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: nil, Handlers: nil}})
		instance, _ := service.ContextRuntime("extension")
		release, err := service.BeginContextOperation(t.Context(), "extension", instance)
		require.NoError(t, err)
		closed := make(chan struct{})

		// Act: finish transport shutdown while the Host operation still owns its reservation.
		go func() { service.Close(); close(closed) }()
		synctest.Wait()

		// Assert: manager shutdown waits for release, then completes without a leaked operation count.
		select {
		case <-closed:
			assert.Fail(t, "runtime manager closed before context operation release")
		default:
		}
		release()
		synctest.Wait()
		<-closed
		assert.Zero(t, service.runtimes["extension"].activeExecutions)
	})
}

// TestRuntimeExitReportsAfterAllContextOperations verifies one exit issue follows the last active reservation.
func TestRuntimeExitReportsAfterAllContextOperations(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange: keep two context operations active when the accepted process exits.
		controller := gomock.NewController(t)
		catalog := NewMockCatalog(controller)
		factory := NewMockRuntimeFactory(controller)
		runtime := NewMockExtensionRuntime(controller)
		catalog.EXPECT().
			Discover(gomock.Any(), gomock.Any()).
			Return(Discovery{Candidates: []Candidate{{ID: "extension", Path: "/extension", InstanceID: ""}}, Issues: nil}, nil)
		factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
		runtime.EXPECT().Register(gomock.Any()).Return(startup.PendingRegistration{}, nil)
		done := make(chan struct{})
		runtime.EXPECT().Done().Return(done)
		runtime.EXPECT().Close()
		reports := 0
		service := New(
			catalog,
			factory,
			func(context.Context, extension.RuntimeFailure) error { reports++; return nil },
		)
		_, err := service.LoadPending(t.Context(), startup.Directory{})
		require.NoError(t, err)
		service.Accept([]startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: nil, Handlers: nil}})
		instance, _ := service.ContextRuntime("extension")
		first, err := service.BeginContextOperation(t.Context(), "extension", instance)
		require.NoError(t, err)
		second, err := service.BeginContextOperation(t.Context(), "extension", instance)
		require.NoError(t, err)
		service.Activate(t.Context())

		// Act: observe process exit, then finish only the first context operation.
		close(done)
		synctest.Wait()
		first()

		// Assert: the exit issue waits for all active work and is emitted exactly once.
		assert.Zero(t, reports)
		second()
		synctest.Wait()
		assert.Equal(t, 1, reports)
		service.Close()
	})
}

// TestFreshRuntimeManagersNeverReuseInstanceIDs verifies process identity remains distinct when Host ownership is
// recreated.
func TestFreshRuntimeManagersNeverReuseInstanceIDs(t *testing.T) {
	t.Parallel()

	// Arrange: create independent managers for the same discovered extension.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().
		Discover(gomock.Any(), gomock.Any()).
		Return(Discovery{Candidates: []Candidate{{ID: "extension", Path: "/extension", InstanceID: ""}}, Issues: nil}, nil).
		Times(2)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil).Times(2)
	runtime.EXPECT().Register(gomock.Any()).Return(startup.PendingRegistration{}, nil).Times(2)
	runtime.EXPECT().Close().Times(2)
	instances := make([]string, 0, 2)

	// Act: start and accept the first process instance under each manager.
	for range 2 {
		service := New(catalog, factory, discardRuntimeFailure)
		_, err := service.LoadPending(t.Context(), startup.Directory{})
		require.NoError(t, err)
		service.Accept([]startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: nil, Handlers: nil}})
		instance, available := service.ContextRuntime("extension")
		require.True(t, available)
		instances = append(instances, instance)
		service.Close()
	}

	// Assert: instance identity is not reused when a manager's in-memory counter would restart.
	assert.NotEqual(t, instances[0], instances[1])
}

// runtimeBindingForTest constructs invocation identity for the accepted mocked runtime.
func runtimeBindingForTest(service *Service, extensionID string) extension.Context {
	instance, _ := service.ContextRuntime(extensionID)
	return extension.Context{
		ID:                "binding",
		ExtensionID:       extensionID,
		RuntimeInstanceID: instance,
		SessionID:         "session",
		WorkingDirectory:  "/project",
	}
}

// discardRuntimeFailure accepts one failure in tests that do not exercise delivery.
func discardRuntimeFailure(context.Context, extension.RuntimeFailure) error { return nil }
