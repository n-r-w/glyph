//go:build !integration

package extensionruntime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

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
		Return(Discovery{Candidates: []Candidate{{ID: "tools", Path: "/tools"}}, Issues: nil}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "tools", Path: "/tools"}).Return(runtime, nil)
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
	runtime.EXPECT().Close()
	service.Close()
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
		Return(Discovery{Candidates: []Candidate{{ID: "bad", Path: "/bad"}}, Issues: nil}, nil)
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
		Return(Discovery{Candidates: []Candidate{{ID: "tools", Path: "/tools"}}, Issues: nil}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(gomock.Any()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().
		Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any()).
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
		func(tool.Progress) error { return nil },
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
		Return(Discovery{Candidates: []Candidate{{ID: "tools", Path: "/tools"}}, Issues: nil}, nil)
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
		Return(Discovery{Candidates: []Candidate{{ID: "tools", Path: "/tools"}}, Issues: nil}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(gomock.Any()).
		Return(startup.PendingRegistration{ID: "", Path: "", Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().
		Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any()).
		DoAndReturn(func(context.Context, string, []byte, tool.ProgressHandler) (tool.Result, error) {
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
			func(tool.Progress) error { return nil },
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

// discardRuntimeFailure accepts one failure in tests that do not exercise delivery.
func discardRuntimeFailure(context.Context, extension.RuntimeFailure) error { return nil }
