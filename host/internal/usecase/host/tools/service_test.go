package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// serviceContextKey isolates context-propagation evidence within this test package.
type serviceContextKey struct{}

// ServiceSuite shares catalog, runtime factory, and mock lifecycle setup across service scenarios.
type ServiceSuite struct {
	suite.Suite
	controller *gomock.Controller
	catalog    *MockCatalog
	factory    *MockRuntimeFactory
}

// SetupTest creates isolated service dependencies for each lifecycle scenario.
func (s *ServiceSuite) SetupTest() {
	s.controller = gomock.NewController(s.T())
	s.catalog = NewMockCatalog(s.controller)
	s.factory = NewMockRuntimeFactory(s.controller)
}

// TestServiceLoadIsolatesCollisions removes every conflicting extension while retaining unaffected tools.
func (s *ServiceSuite) TestServiceLoadIsolatesCollisions() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	first := NewMockExtensionRuntime(controller)
	second := NewMockExtensionRuntime(controller)
	unaffected := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{Candidates: []Candidate{{ID: "first", Path: "/first"}, {ID: "second", Path: "/second"}, {ID: "other", Path: "/other"}}, Issues: nil}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "first", Path: "/first"}).Return(first, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "second", Path: "/second"}).Return(second, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "other", Path: "/other"}).Return(unaffected, nil)
	first.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("shared"), testDescriptor("first-only")}, nil)
	second.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("shared"), testDescriptor("second-only")}, nil)
	unaffected.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("safe")}, nil)
	first.EXPECT().Close()
	second.EXPECT().Close()
	service := New(catalog, factory, discardRuntimeFailure)

	report, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})

	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, []string{"first", "second"}, report.Issues[0].PluginIDs)
	assert.Equal(t, []LoadedExtension{{
		ID: "other", Path: "/other", Tools: []tool.Descriptor{testDescriptor("safe")},
	}}, report.Extensions)
	assert.Equal(t, []tool.Descriptor{testDescriptor("safe")}, service.Tools())
	unaffected.EXPECT().Close()
	service.Close()
}

// TestServiceLoadReportsMultipleExtensionsInIDOrder preserves per-extension tool ownership.
func (s *ServiceSuite) TestServiceLoadReportsMultipleExtensionsInIDOrder() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	first := NewMockExtensionRuntime(controller)
	second := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(
		Discovery{Candidates: []Candidate{{ID: "second", Path: "/second"}, {ID: "first", Path: "/first"}}, Issues: nil}, nil,
	)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "second", Path: "/second"}).Return(second, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "first", Path: "/first"}).Return(first, nil)
	second.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("bash")}, nil)
	first.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
	service := New(catalog, factory, discardRuntimeFailure)

	report, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})

	require.NoError(t, err)
	assert.Equal(t, []LoadedExtension{
		{ID: "first", Path: "/first", Tools: []tool.Descriptor{testDescriptor("read")}},
		{ID: "second", Path: "/second", Tools: []tool.Descriptor{testDescriptor("bash")}},
	}, report.Extensions)
	first.EXPECT().Close()
	second.EXPECT().Close()
	service.Close()
}

// TestServiceExecuteRemovesFailedRuntime and returns terminal unavailable results for late calls.
func (s *ServiceSuite) TestServiceExecuteRemovesFailedRuntime() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "", Explicit: false}).Return(Discovery{Candidates: []Candidate{{ID: "broken", Path: "/broken"}}, Issues: nil}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "broken", Path: "/broken"}).Return(runtime, nil)
	runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
	service := New(catalog, factory, discardRuntimeFailure)
	require.Empty(t, mustLoad(t, service))
	runtime.EXPECT().Execute(t.Context(), "read", []byte(`{}`), gomock.Any()).Return(
		tool.Result{Content: "", IsError: false},
		fmt.Errorf("process crashed: %w", ErrExtensionUnavailable),
	)
	runtime.EXPECT().Close()

	call := model.ToolCall{ID: "call-read", Name: "read", Arguments: map[string]any{}}
	_, err := service.Execute(t.Context(), call, discardProgress)
	require.Error(t, err)
	assert.Empty(t, service.Tools())

	late, err := service.Execute(t.Context(), call, discardProgress)
	require.NoError(t, err)
	assert.True(t, late.IsError)
	assert.ErrorContains(t, errors.New(late.Content), "unavailable")
}

// TestServiceExecutePreservesRuntimeOnProgressDeliveryFailure keeps a healthy owner registered.
func (s *ServiceSuite) TestServiceExecutePreservesRuntimeOnProgressDeliveryFailure() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "", Explicit: false}).Return(
		Discovery{Candidates: []Candidate{{ID: "healthy", Path: "/healthy"}}, Issues: nil}, nil,
	)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "healthy", Path: "/healthy"}).Return(runtime, nil)
	runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("bash")}, nil)
	service := New(catalog, factory, discardRuntimeFailure)
	require.Empty(t, mustLoad(t, service))
	deliveryErr := errors.New("event consumer failed")
	runtime.EXPECT().Execute(
		t.Context(),
		"bash",
		[]byte(`{"enabled":true,"items":[1,"two",null],"nested":{"value":3.5}}`),
		gomock.Any(),
	).DoAndReturn(
		func(_ context.Context, _ string, _ []byte, handler tool.ProgressHandler) (tool.Result, error) {
			require.ErrorIs(t, handler(tool.Progress{Channel: tool.ProgressChannelStdout, Content: "partial"}), deliveryErr)
			return tool.Result{Content: "", IsError: false}, fmt.Errorf("deliver progress: %w", deliveryErr)
		},
	)

	call := model.ToolCall{
		ID:   "call-bash",
		Name: "bash",
		Arguments: map[string]any{
			"enabled": true,
			"items":   []any{float64(1), "two", nil},
			"nested":  map[string]any{"value": 3.5},
		},
	}
	_, err := service.Execute(t.Context(), call, func(tool.Progress) error { return deliveryErr })
	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, []tool.Descriptor{testDescriptor("bash")}, service.Tools())

	runtime.EXPECT().Execute(
		t.Context(),
		"bash",
		[]byte(`{"enabled":true,"items":[1,"two",null],"nested":{"value":3.5}}`),
		gomock.Any(),
	).Return(
		tool.Result{Content: "ok", IsError: false}, nil,
	)
	result, err := service.Execute(t.Context(), call, discardProgress)
	require.NoError(t, err)
	assert.Equal(t, agent.ToolResult{
		CallID: "call-bash", ToolName: "bash", Content: "ok", IsError: false,
	}, result)
	runtime.EXPECT().Close()
	service.Close()
}

// TestServiceReportsIdleRuntimeExitOnceAndKeepsOtherExtensions verifies post-start failure isolation.
func (s *ServiceSuite) TestServiceReportsIdleRuntimeExitOnceAndKeepsOtherExtensions() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	crashed := NewMockExtensionRuntime(controller)
	healthy := NewMockExtensionRuntime(controller)
	crashedDone := make(chan struct{})
	healthyDone := make(chan struct{})
	crashedClosed := make(chan struct{})
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "crashed-plugin", Path: "/plugins/crashed"}, {ID: "healthy-plugin", Path: "/plugins/healthy"}},
		Issues:     nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "crashed-plugin", Path: "/plugins/crashed"}).Return(crashed, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "healthy-plugin", Path: "/plugins/healthy"}).Return(healthy, nil)
	crashed.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
	healthy.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("bash")}, nil)
	crashed.EXPECT().Done().Return(crashedDone)
	healthy.EXPECT().Done().Return(healthyDone)
	crashed.EXPECT().Close().Do(func() { close(crashedClosed) })
	contextKey := serviceContextKey{}
	activationContext, cancelActivation := context.WithCancel(context.WithValue(t.Context(), contextKey, "monitor-value"))
	failures := make([]tool.RuntimeFailure, 0, 1)
	reportedContexts := make(chan context.Context, 1)
	service := New(catalog, factory, func(ctx context.Context, failure tool.RuntimeFailure) error {
		failures = append(failures, failure)
		reportedContexts <- ctx
		return nil
	})
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	service.Activate(activationContext)
	cancelActivation()

	close(crashedDone)
	<-crashedClosed

	reportedContext := <-reportedContexts
	require.NoError(t, reportedContext.Err())
	assert.Equal(t, "monitor-value", reportedContext.Value(contextKey))
	assert.Equal(t, []tool.RuntimeFailure{{
		PluginID: "crashed-plugin", Condition: tool.RuntimeUnavailableProcessExited,
	}}, failures)
	assert.Equal(t, []tool.Descriptor{testDescriptor("bash")}, service.Tools())
	healthy.EXPECT().Execute(t.Context(), "bash", []byte(`{}`), gomock.Any()).Return(
		tool.Result{Content: "ok", IsError: false}, nil,
	)
	result, executeErr := service.Execute(t.Context(), model.ToolCall{ID: "call", Name: "bash", Arguments: map[string]any{}}, discardProgress)
	require.NoError(t, executeErr)
	assert.Equal(t, "ok", result.Content)
	healthy.EXPECT().Close()
	service.Close()
}

// TestServiceKeepsActiveUnavailabilityOnTheToolResult verifies crash reporting has one presentation owner.
func (s *ServiceSuite) TestServiceKeepsActiveUnavailabilityOnTheToolResult() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	started := make(chan struct{})
	allowResult := make(chan struct{})
	closed := make(chan struct{})
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "active-plugin", Path: "/plugins/active"}}, Issues: nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "active-plugin", Path: "/plugins/active"}).Return(runtime, nil)
	runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any()).DoAndReturn(
		func(context.Context, string, []byte, tool.ProgressHandler) (tool.Result, error) {
			close(started)
			<-allowResult
			return tool.Result{}, fmt.Errorf("process exited: %w", ErrExtensionUnavailable)
		},
	)
	runtime.EXPECT().Close().Do(func() { close(closed) })
	failures := make(chan tool.RuntimeFailure, 2)
	service := New(catalog, factory, func(_ context.Context, failure tool.RuntimeFailure) error {
		failures <- failure
		return nil
	})
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	service.Activate(t.Context())
	execution := make(chan error, 1)
	go func() {
		_, executeErr := service.Execute(t.Context(), model.ToolCall{
			ID: "call", Name: "read", Arguments: map[string]any{},
		}, discardProgress)
		execution <- executeErr
	}()
	<-started

	close(done)
	<-closed
	close(allowResult)

	require.ErrorIs(t, <-execution, ErrExtensionUnavailable)
	select {
	case failure := <-failures:
		assert.Equal(t, tool.RuntimeFailure{
			PluginID: "active-plugin", Condition: tool.RuntimeUnavailableProcessExited,
		}, failure)
	default:
		assert.Fail(t, "active runtime failure was not reported")
	}
	select {
	case duplicate := <-failures:
		assert.Fail(t, "runtime failure was reported more than once", "duplicate: %+v", duplicate)
	default:
	}
	assert.Empty(t, service.Tools())
}

// TestServiceReportsActiveUnavailabilityBeforeDoneObservationOnce verifies RPC-first crash ownership.
func (s *ServiceSuite) TestServiceReportsActiveUnavailabilityBeforeDoneObservationOnce() {
	synctest.Test(s.T(), func(t *testing.T) {
		controller := s.controller
		catalog := s.catalog
		factory := s.factory
		runtime := NewMockExtensionRuntime(controller)
		done := make(chan struct{})
		catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
			Candidates: []Candidate{{ID: "rpc-first-plugin", Path: "/plugins/rpc-first"}}, Issues: nil,
		}, nil)
		factory.EXPECT().Start(t.Context(), Candidate{ID: "rpc-first-plugin", Path: "/plugins/rpc-first"}).Return(runtime, nil)
		runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
		runtime.EXPECT().Done().Return(done)
		runtime.EXPECT().Execute(t.Context(), "read", []byte(`{}`), gomock.Any()).Return(
			tool.Result{Content: "", IsError: false}, fmt.Errorf("process exited: %w", ErrExtensionUnavailable),
		)
		runtime.EXPECT().Close()
		failures := make([]tool.RuntimeFailure, 0, 1)
		service := New(catalog, factory, func(_ context.Context, failure tool.RuntimeFailure) error {
			failures = append(failures, failure)
			return nil
		})
		_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
		require.NoError(t, err)
		service.Activate(t.Context())

		_, executeErr := service.Execute(t.Context(), model.ToolCall{
			ID: "call", Name: "read", Arguments: map[string]any{},
		}, discardProgress)
		require.ErrorIs(t, executeErr, ErrExtensionUnavailable)
		assert.Equal(t, []tool.RuntimeFailure{{
			PluginID: "rpc-first-plugin", Condition: tool.RuntimeUnavailableProcessExited,
		}}, failures)

		close(done)
		synctest.Wait()

		assert.Len(t, failures, 1)
		assert.Empty(t, service.Tools())
	})
}

// TestServiceReportsExitAfterSuccessfulActiveExecution verifies deferred crash ownership.
func (s *ServiceSuite) TestServiceReportsExitAfterSuccessfulActiveExecution() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	started := make(chan struct{})
	allowResult := make(chan struct{})
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "successful-plugin", Path: "/plugins/successful"}}, Issues: nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "successful-plugin", Path: "/plugins/successful"}).Return(runtime, nil)
	runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any()).DoAndReturn(
		func(context.Context, string, []byte, tool.ProgressHandler) (tool.Result, error) {
			close(started)
			<-allowResult
			return tool.Result{Content: "ok", IsError: false}, nil
		},
	)
	closed := make(chan struct{})
	runtime.EXPECT().Close().Do(func() { close(closed) })
	failures := make(chan tool.RuntimeFailure, 1)
	service := New(catalog, factory, func(_ context.Context, failure tool.RuntimeFailure) error {
		failures <- failure
		return nil
	})
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	service.Activate(t.Context())
	execution := make(chan error, 1)
	go func() {
		_, executeErr := service.Execute(t.Context(), model.ToolCall{
			ID: "call", Name: "read", Arguments: map[string]any{},
		}, discardProgress)
		execution <- executeErr
	}()
	<-started

	close(done)
	<-closed
	close(allowResult)

	require.NoError(t, <-execution)
	assert.Equal(t, tool.RuntimeFailure{
		PluginID: "successful-plugin", Condition: tool.RuntimeUnavailableProcessExited,
	}, <-failures)
	assert.Empty(t, service.Tools())
}

// TestServicePlannedCloseDoesNotReportRuntimeFailure verifies active shutdown remains silent.
func (s *ServiceSuite) TestServicePlannedCloseDoesNotReportRuntimeFailure() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	started := make(chan struct{})
	allowResult := make(chan struct{})
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "planned-plugin", Path: "/plugins/planned"}}, Issues: nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "planned-plugin", Path: "/plugins/planned"}).Return(runtime, nil)
	runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("read")}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any()).DoAndReturn(
		func(context.Context, string, []byte, tool.ProgressHandler) (tool.Result, error) {
			close(started)
			<-allowResult
			return tool.Result{}, fmt.Errorf("process exited: %w", ErrExtensionUnavailable)
		},
	)
	runtime.EXPECT().Close().Do(func() { close(done) })
	failures := make([]tool.RuntimeFailure, 0)
	service := New(catalog, factory, func(_ context.Context, failure tool.RuntimeFailure) error {
		failures = append(failures, failure)
		return nil
	})
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	service.Activate(t.Context())
	execution := make(chan error, 1)
	go func() {
		_, executeErr := service.Execute(t.Context(), model.ToolCall{
			ID: "call", Name: "read", Arguments: map[string]any{},
		}, discardProgress)
		execution <- executeErr
	}()
	<-started

	service.Close()
	close(allowResult)

	require.ErrorIs(t, <-execution, ErrExtensionUnavailable)
	assert.Empty(t, failures)
	assert.Empty(t, service.Tools())
}

// TestServiceRemovesToolsWhenRuntimeExits handles an idle process crash without a restart.
func (s *ServiceSuite) TestServiceRemovesToolsWhenRuntimeExits() {
	t := s.T()

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	closed := make(chan struct{})
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "", Explicit: false}).Return(
		Discovery{Candidates: []Candidate{{ID: "crash", Path: "/crash"}}, Issues: nil}, nil,
	)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "crash", Path: "/crash"}).Return(runtime, nil)
	runtime.EXPECT().ListTools(t.Context()).Return([]tool.Descriptor{testDescriptor("bash")}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Close().Do(func() { close(closed) })
	service := New(catalog, factory, discardRuntimeFailure)
	require.Empty(t, mustLoad(t, service))
	service.Activate(t.Context())

	close(done)
	<-closed

	assert.Empty(t, service.Tools())
}

// mustLoad loads the empty directory input for focused execution tests.
func mustLoad(t *testing.T, service *Service) []Issue {
	t.Helper()
	report, err := service.Load(t.Context(), Directory{Path: "", Explicit: false})
	require.NoError(t, err)
	return report.Issues
}

// TestServiceSuite runs isolated tool service lifecycle scenarios.
func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// testDescriptor creates a complete descriptor for registry behavior tests.
func testDescriptor(name string) tool.Descriptor {
	return tool.Descriptor{
		Name: name, Description: "test tool", InputSchemaJSON: []byte(`{}`),
		ConstrainedSampling: tool.ConstrainedSampling{
			Kind: 0, JSONSchemaStrictness: 0,
			Grammar: tool.GrammarVariants{Lark: "", Regex: ""}, GrammarInputProperty: "",
		},
	}
}

// discardProgress accepts progress when only terminal behavior matters.
func discardProgress(tool.Progress) error { return nil }

// discardRuntimeFailure keeps tests that do not exercise runtime reporting focused.
func discardRuntimeFailure(context.Context, tool.RuntimeFailure) error { return nil }
