//go:build !integration

package extensions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// serviceContextKey isolates context-propagation evidence within this test package.
type serviceContextKey struct{}

// ServiceSuite shares catalog, runtime factory, and mock lifecycle setup across service scenarios.
type ServiceSuite struct {
	// Suite provides Testify suite lifecycle and assertions.
	suite.Suite
	// controller owns mocks created for the current suite test.
	controller *gomock.Controller
	// catalog discovers extension candidates for the current suite test.
	catalog *MockCatalog
	// factory starts extension runtimes for the current suite test.
	factory *MockRuntimeFactory
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

	// Arrange: configure two conflicting registrations and one unaffected registration.

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	first := NewMockExtensionRuntime(controller)
	second := NewMockExtensionRuntime(controller)
	unaffected := NewMockExtensionRuntime(controller)
	catalog.EXPECT().
		Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).
		Return(Discovery{
			Candidates: []Candidate{
				{ID: "first", Path: "/first"},
				{ID: "second", Path: "/second"},
				{ID: "other", Path: "/other"},
			},
			Issues: nil,
		}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "first", Path: "/first"}).Return(first, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "second", Path: "/second"}).Return(second, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "other", Path: "/other"}).Return(unaffected, nil)
	first.EXPECT().
		Register(t.Context()).
		Return(Registration{
			Tools: []tool.Descriptor{testDescriptor("shared"), testDescriptor("first-only")}, Handlers: nil,
		}, nil)
	second.EXPECT().
		Register(t.Context()).
		Return(Registration{
			Tools: []tool.Descriptor{testDescriptor("shared"), testDescriptor("second-only")}, Handlers: nil,
		}, nil)
	unaffected.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("safe")}, Handlers: nil}, nil)
	first.EXPECT().Close()
	second.EXPECT().Close()
	service := New(catalog, factory, discardRuntimeFailure)

	// Act: load the discovered extensions.
	report, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})

	// Assert: retain only the unaffected tool and report both collisions.
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	assert.Equal(t, []string{"first", "second"}, report.Issues[0].PluginIDs)
	assert.Equal(t, []LoadedExtension{{
		ID: "other", Path: "/other", Tools: []tool.Descriptor{testDescriptor("safe")}, Handlers: nil,
	}}, report.Extensions)
	assert.Equal(t, []tool.Descriptor{testDescriptor("safe")}, service.Tools())
	unaffected.EXPECT().Close()
	service.Close()
}

// TestServiceLoadReportsMultipleExtensionsInIDOrder preserves per-extension tool ownership.
func (s *ServiceSuite) TestServiceLoadReportsMultipleExtensionsInIDOrder() {
	t := s.T()

	// Arrange: configure two successful extensions discovered out of ID order.

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	first := NewMockExtensionRuntime(controller)
	second := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(
		Discovery{
			Candidates: []Candidate{{ID: "second", Path: "/second"}, {ID: "first", Path: "/first"}},
			Issues:     nil,
		}, nil,
	)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "second", Path: "/second"}).Return(second, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "first", Path: "/first"}).Return(first, nil)
	second.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("bash")}, Handlers: nil}, nil)
	first.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
	service := New(catalog, factory, discardRuntimeFailure)

	// Act: load both successful extensions.
	report, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})

	// Assert: return both extensions in ID order.
	require.NoError(t, err)
	assert.Equal(t, []LoadedExtension{
		{ID: "first", Path: "/first", Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil},
		{ID: "second", Path: "/second", Tools: []tool.Descriptor{testDescriptor("bash")}, Handlers: nil},
	}, report.Extensions)
	first.EXPECT().Close()
	second.EXPECT().Close()
	service.Close()
}

// TestServicePreservesHandlerRegistrationOrder verifies activation order followed by declaration order.
func (s *ServiceSuite) TestServicePreservesHandlerRegistrationOrder() {
	t := s.T()

	// Arrange two extensions with ordered handler declarations.
	first := NewMockExtensionRuntime(s.controller)
	second := NewMockExtensionRuntime(s.controller)
	s.catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "first", Path: "/first"}, {ID: "second", Path: "/second"}},
		Issues:     nil,
	}, nil)
	s.factory.EXPECT().Start(t.Context(), Candidate{ID: "first", Path: "/first"}).Return(first, nil)
	s.factory.EXPECT().Start(t.Context(), Candidate{ID: "second", Path: "/second"}).Return(second, nil)
	first.EXPECT().Register(t.Context()).Return(Registration{Tools: nil, Handlers: []HandlerDescriptor{
		{ID: "request-a", Kind: HandlerKindSessionBeforeTreeRequest},
		{ID: "request-b", Kind: HandlerKindSessionBeforeTreeRequest},
	}}, nil)
	second.EXPECT().Register(t.Context()).Return(Registration{Tools: nil, Handlers: []HandlerDescriptor{
		{ID: "request-c", Kind: HandlerKindSessionBeforeTreeRequest},
	}}, nil)
	service := New(s.catalog, s.factory, discardRuntimeFailure)

	// Act by loading both complete registrations.
	report, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})

	// Assert activation order followed by declaration order.
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	assert.Equal(t, []RegisteredHandler{
		{ExtensionID: "first", ID: "request-a", Kind: HandlerKindSessionBeforeTreeRequest},
		{ExtensionID: "first", ID: "request-b", Kind: HandlerKindSessionBeforeTreeRequest},
		{ExtensionID: "second", ID: "request-c", Kind: HandlerKindSessionBeforeTreeRequest},
	}, service.registeredHandlers(HandlerKindSessionBeforeTreeRequest))
	first.EXPECT().Close()
	second.EXPECT().Close()
	service.Close()
}

// TestServiceRejectsInvalidHandlerRegistration verifies invalid IDs, kinds, and duplicates prevent publication.
func (s *ServiceSuite) TestServiceRejectsInvalidHandlerRegistration() {
	// Arrange invalid registration shapes.
	testCases := map[string][]HandlerDescriptor{
		"empty ID":     {{ID: "", Kind: HandlerKindSessionTree}},
		"unknown kind": {{ID: "observer", Kind: HandlerKind(99)}},
		"duplicate IDs": {
			{ID: "same", Kind: HandlerKindSessionTree},
			{ID: "same", Kind: HandlerKindSessionBeforeTreeRequest},
		},
	}
	for name, handlers := range testCases {
		s.Run(name, func() {
			t := s.T()
			controller := gomock.NewController(t)
			catalog := NewMockCatalog(controller)
			factory := NewMockRuntimeFactory(controller)
			runtime := NewMockExtensionRuntime(controller)
			catalog.EXPECT().Discover(gomock.Any(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
				Candidates: []Candidate{{ID: "invalid", Path: "/invalid"}}, Issues: nil,
			}, nil)
			factory.EXPECT().Start(gomock.Any(), Candidate{ID: "invalid", Path: "/invalid"}).Return(runtime, nil)
			runtime.EXPECT().Register(gomock.Any()).Return(Registration{Tools: nil, Handlers: handlers}, nil)
			runtime.EXPECT().Close()
			service := New(catalog, factory, discardRuntimeFailure)

			// Act by loading the invalid complete registration.
			report, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})

			// Assert the process closes before its registration is published.
			require.NoError(t, err)
			require.Len(t, report.Issues, 1)
			assert.Empty(t, report.Extensions)
			assert.Empty(t, service.registeredHandlers(HandlerKindSessionTree))
		})
	}
}

// TestServiceHandleDispatchesTypedPayload verifies handler operations use the registered ID and kind.
func (s *ServiceSuite) TestServiceHandleDispatchesTypedPayload() {
	t := s.T()

	// Arrange one registered observer and its typed Host invocation.
	runtime := NewMockExtensionRuntime(s.controller)
	s.catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "tree", Path: "/tree"}}, Issues: nil,
	}, nil)
	s.factory.EXPECT().Start(t.Context(), Candidate{ID: "tree", Path: "/tree"}).Return(runtime, nil)
	runtime.EXPECT().Register(t.Context()).Return(Registration{Tools: nil, Handlers: []HandlerDescriptor{
		{ID: "observer", Kind: HandlerKindSessionTree},
	}}, nil)
	service := New(s.catalog, s.factory, discardRuntimeFailure)
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	handler := RegisteredHandler{ExtensionID: "tree", ID: "observer", Kind: HandlerKindSessionTree}
	invocation := SessionTreeInvocation{
		SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
		NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
	}
	request := HandlerRequest{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestInvocation](),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultInvocation](),
		SessionTree:              mo.Some(invocation),
	}
	runtime.EXPECT().Handle(t.Context(), "observer", request).Return(HandlerResponse{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestAction](),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultAction](),
		SessionTree:              mo.Some(SessionTreeAction{}),
	}, nil)

	// Act by dispatching the registered handler.
	response, err := service.handle(t.Context(), handler, request)

	// Assert the typed response is returned unchanged.
	require.NoError(t, err)
	assert.Equal(t, HandlerResponse{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestAction](),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultAction](),
		SessionTree:              mo.Some(SessionTreeAction{}),
	}, response)
	runtime.EXPECT().Close()
	service.Close()
}

// TestServiceHandlePreservesRuntimeOnOrdinaryError verifies one handler failure does not change availability.
func (s *ServiceSuite) TestServiceHandlePreservesRuntimeOnOrdinaryError() {
	t := s.T()

	// Arrange one registered observer that returns an ordinary handler error.
	runtime := NewMockExtensionRuntime(s.controller)
	s.catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "tree", Path: "/tree"}}, Issues: nil,
	}, nil)
	s.factory.EXPECT().Start(t.Context(), Candidate{ID: "tree", Path: "/tree"}).Return(runtime, nil)
	runtime.EXPECT().Register(t.Context()).Return(Registration{Tools: nil, Handlers: []HandlerDescriptor{
		{ID: "observer", Kind: HandlerKindSessionTree},
	}}, nil)
	service := New(s.catalog, s.factory, discardRuntimeFailure)
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	handler := RegisteredHandler{ExtensionID: "tree", ID: "observer", Kind: HandlerKindSessionTree}
	invocation := SessionTreeInvocation{
		SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
		NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
	}
	request := HandlerRequest{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestInvocation](),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultInvocation](),
		SessionTree:              mo.Some(invocation),
	}
	handlerErr := errors.New("handler failed")
	runtime.EXPECT().Handle(t.Context(), "observer", request).Return(HandlerResponse{}, handlerErr)

	// Act by dispatching the failing handler.
	response, err := service.handle(t.Context(), handler, request)

	// Assert the error is returned without changing runtime availability.
	assert.Empty(t, response)
	require.ErrorIs(t, err, handlerErr)
	assert.Equal(t, []RegisteredHandler{handler}, service.registeredHandlers(HandlerKindSessionTree))
	runtime.EXPECT().Close()
	service.Close()
}

// TestServiceHandleDisablesKindMismatch verifies a protocol response for another kind removes the extension.
func (s *ServiceSuite) TestServiceHandleDisablesKindMismatch() {
	t := s.T()

	// Arrange one observer that violates its registered response kind.
	runtime := NewMockExtensionRuntime(s.controller)
	s.catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "tree", Path: "/tree"}}, Issues: nil,
	}, nil)
	s.factory.EXPECT().Start(t.Context(), Candidate{ID: "tree", Path: "/tree"}).Return(runtime, nil)
	runtime.EXPECT().Register(t.Context()).Return(Registration{Tools: nil, Handlers: []HandlerDescriptor{
		{ID: "observer", Kind: HandlerKindSessionTree},
	}}, nil)
	failures := make([]tool.RuntimeFailure, 0, 1)
	service := New(s.catalog, s.factory, func(_ context.Context, failure tool.RuntimeFailure) error {
		failures = append(failures, failure)
		return nil
	})
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	handler := RegisteredHandler{ExtensionID: "tree", ID: "observer", Kind: HandlerKindSessionTree}
	invocation := SessionTreeInvocation{
		SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
		NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
	}
	request := HandlerRequest{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestInvocation](),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultInvocation](),
		SessionTree:              mo.Some(invocation),
	}
	runtime.EXPECT().Handle(t.Context(), "observer", request).Return(HandlerResponse{
		SessionBeforeTreeRequest: mo.Some(SessionBeforeTreeRequestAction{
			Cancel: false, RequestAction: RequestActionPreserve,
			Request: mo.None[NavigationRequest](), ResultAction: ResultActionPreserve,
			Result: mo.None[BranchSummaryResult](),
		}), SessionBeforeTreeResult: mo.None[SessionBeforeTreeResultAction](),
		SessionTree: mo.None[SessionTreeAction](),
	}, nil)
	runtime.EXPECT().Close()

	// Act by dispatching the protocol-violating handler.
	response, err := service.handle(t.Context(), handler, request)

	// Assert the protocol failure removes the runtime and reports its loss.
	assert.Empty(t, response)
	require.ErrorIs(t, err, ErrExtensionUnavailable)
	assert.Empty(t, service.registeredHandlers(HandlerKindSessionTree))
	assert.Equal(t, []tool.RuntimeFailure{{
		PluginID: "tree", Condition: tool.RuntimeUnavailableProcessExited,
	}}, failures)
}

// TestServiceExecuteRemovesFailedRuntime and returns terminal unavailable results for late calls.
func (s *ServiceSuite) TestServiceExecuteRemovesFailedRuntime() {
	t := s.T()

	// Arrange: configure one registered tool runtime that returns unavailability.

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().
		Discover(t.Context(), Directory{Path: "", Explicit: false}).
		Return(Discovery{Candidates: []Candidate{{ID: "broken", Path: "/broken"}}, Issues: nil}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "broken", Path: "/broken"}).Return(runtime, nil)
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
	service := New(catalog, factory, discardRuntimeFailure)
	require.Empty(t, mustLoad(t, service))
	runtime.EXPECT().Execute(t.Context(), "read", []byte(`{}`), gomock.Any()).Return(
		tool.Result{Contents: tool.TextContents(""), IsError: false},
		fmt.Errorf("process crashed: %w", ErrExtensionUnavailable),
	)
	runtime.EXPECT().Close()

	call := model.ToolCall{ID: "call-read", Name: "read", Arguments: map[string]any{}}
	// Act: execute the failing tool call.
	_, err := service.Execute(t.Context(), call, discardProgress)
	// Assert: remove the failed runtime and return unavailable data to a later call.
	require.Error(t, err)
	assert.Empty(t, service.Tools())

	late, err := service.Execute(t.Context(), call, discardProgress)
	require.NoError(t, err)
	assert.True(t, late.IsError)
	assert.ErrorContains(t, errors.New(late.Contents[0].Text.OrEmpty()), "unavailable")
}

// TestServiceExecutePreservesRuntimeOnProgressDeliveryFailure keeps a healthy owner registered.
func (s *ServiceSuite) TestServiceExecutePreservesRuntimeOnProgressDeliveryFailure() {
	t := s.T()

	// Arrange: configure one healthy runtime whose progress callback fails.

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "", Explicit: false}).Return(
		Discovery{Candidates: []Candidate{{ID: "healthy", Path: "/healthy"}}, Issues: nil}, nil,
	)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "healthy", Path: "/healthy"}).Return(runtime, nil)
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("bash")}, Handlers: nil}, nil)
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
			require.ErrorIs(
				t,
				handler(tool.Progress{Channel: tool.ProgressChannelStdout, Content: "partial"}),
				deliveryErr,
			)
			result := tool.Result{
				Contents: tool.TextContents(""),
				IsError:  false,
			}
			return result, fmt.Errorf("deliver progress: %w", deliveryErr)
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
	// Act: execute the tool with the failing progress callback.
	_, err := service.Execute(t.Context(), call, func(tool.Progress) error { return deliveryErr })
	// Assert: preserve the callback cause and keep the runtime registered.
	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, []tool.Descriptor{testDescriptor("bash")}, service.Tools())

	runtime.EXPECT().Execute(
		t.Context(),
		"bash",
		[]byte(`{"enabled":true,"items":[1,"two",null],"nested":{"value":3.5}}`),
		gomock.Any(),
	).Return(
		tool.Result{Contents: tool.TextContents("ok"), IsError: false}, nil,
	)
	result, err := service.Execute(t.Context(), call, discardProgress)
	require.NoError(t, err)
	assert.Equal(t, agent.ToolResult{
		CallID: "call-bash", ToolName: "bash", Contents: tool.TextContents("ok"), IsError: false,
	}, result)
	runtime.EXPECT().Close()
	service.Close()
}

// TestServiceReportsIdleRuntimeExitOnceAndKeepsOtherExtensions verifies post-start failure isolation.
func (s *ServiceSuite) TestServiceReportsIdleRuntimeExitOnceAndKeepsOtherExtensions() {
	t := s.T()

	// Arrange: configure one exiting idle runtime and one healthy runtime.

	controller := s.controller
	catalog := s.catalog
	factory := s.factory
	crashed := NewMockExtensionRuntime(controller)
	healthy := NewMockExtensionRuntime(controller)
	crashedDone := make(chan struct{})
	healthyDone := make(chan struct{})
	crashedClosed := make(chan struct{})
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{
			{ID: "crashed-plugin", Path: "/plugins/crashed"},
			{ID: "healthy-plugin", Path: "/plugins/healthy"},
		},
		Issues: nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "crashed-plugin", Path: "/plugins/crashed"}).Return(crashed, nil)
	factory.EXPECT().Start(t.Context(), Candidate{ID: "healthy-plugin", Path: "/plugins/healthy"}).Return(healthy, nil)
	crashed.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
	healthy.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("bash")}, Handlers: nil}, nil)
	crashed.EXPECT().Done().Return(crashedDone)
	healthy.EXPECT().Done().Return(healthyDone)
	crashed.EXPECT().Close().Do(func() { close(crashedClosed) })
	contextKey := serviceContextKey{}
	activationContext, cancelActivation := context.WithCancel(
		context.WithValue(t.Context(), contextKey, "monitor-value"),
	)
	failures := make([]tool.RuntimeFailure, 0, 1)
	reportedContexts := make(chan context.Context, 1)
	service := New(catalog, factory, func(ctx context.Context, failure tool.RuntimeFailure) error {
		failures = append(failures, failure)
		reportedContexts <- ctx
		return nil
	})
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	// Act: activate exit observation.
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
	// Assert: report one failure and retain the healthy tool.
	assert.Equal(t, []tool.Descriptor{testDescriptor("bash")}, service.Tools())
	healthy.EXPECT().Execute(t.Context(), "bash", []byte(`{}`), gomock.Any()).Return(
		tool.Result{Contents: tool.TextContents("ok"), IsError: false}, nil,
	)
	result, executeErr := service.Execute(
		t.Context(),
		model.ToolCall{ID: "call", Name: "bash", Arguments: map[string]any{}},
		discardProgress,
	)
	require.NoError(t, executeErr)
	assert.Equal(t, "ok", result.Contents[0].Text.OrEmpty())
	healthy.EXPECT().Close()
	service.Close()
}

// TestServiceKeepsActiveUnavailabilityOnTheToolResult verifies crash reporting has one presentation owner.
func (s *ServiceSuite) TestServiceKeepsActiveUnavailabilityOnTheToolResult() {
	t := s.T()

	// Arrange: configure an active runtime that becomes unavailable during execution.

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
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
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
	// Act: activate observation and execute its tool.
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

	// Assert: return active unavailability once and remove the failed runtime.
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

// TestServiceReportsActiveUnavailabilityBeforeDoneObservationOnce verifies operation-first crash ownership.
func (s *ServiceSuite) TestServiceReportsActiveUnavailabilityBeforeDoneObservationOnce() {
	synctest.Test(s.T(), func(t *testing.T) {
		// Arrange: configure active unavailability before process-exit observation.
		controller := s.controller
		catalog := s.catalog
		factory := s.factory
		runtime := NewMockExtensionRuntime(controller)
		done := make(chan struct{})
		catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
			Candidates: []Candidate{{ID: "operation-first-plugin", Path: "/plugins/operation-first"}}, Issues: nil,
		}, nil)
		factory.EXPECT().
			Start(t.Context(), Candidate{ID: "operation-first-plugin", Path: "/plugins/operation-first"}).
			Return(runtime, nil)
		runtime.EXPECT().
			Register(t.Context()).
			Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
		runtime.EXPECT().Done().Return(done)
		runtime.EXPECT().Execute(t.Context(), "read", []byte(`{}`), gomock.Any()).Return(
			tool.Result{
				Contents: tool.TextContents(""),
				IsError:  false,
			}, fmt.Errorf("process exited: %w", ErrExtensionUnavailable),
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

		// Act: execute through the failing runtime before publishing its exit.
		_, executeErr := service.Execute(t.Context(), model.ToolCall{
			ID: "call", Name: "read", Arguments: map[string]any{},
		}, discardProgress)
		// Assert: report the active failure once and remove the runtime.
		require.ErrorIs(t, executeErr, ErrExtensionUnavailable)
		assert.Equal(t, []tool.RuntimeFailure{{
			PluginID: "operation-first-plugin", Condition: tool.RuntimeUnavailableProcessExited,
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

	// Arrange: configure a runtime that exits after returning a successful active result.

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
	factory.EXPECT().
		Start(t.Context(), Candidate{ID: "successful-plugin", Path: "/plugins/successful"}).
		Return(runtime, nil)
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Execute(gomock.Any(), "read", []byte(`{}`), gomock.Any()).DoAndReturn(
		func(context.Context, string, []byte, tool.ProgressHandler) (tool.Result, error) {
			close(started)
			<-allowResult
			return tool.Result{Contents: tool.TextContents("ok"), IsError: false}, nil
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
	// Act: activate observation and execute its tool.
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

	// Assert: return success before reporting the later process exit.
	require.NoError(t, <-execution)
	assert.Equal(t, tool.RuntimeFailure{
		PluginID: "successful-plugin", Condition: tool.RuntimeUnavailableProcessExited,
	}, <-failures)
	assert.Empty(t, service.Tools())
}

// TestServicePlannedCloseDoesNotReportRuntimeFailure verifies active shutdown remains silent.
func (s *ServiceSuite) TestServicePlannedCloseDoesNotReportRuntimeFailure() {
	t := s.T()

	// Arrange: configure one active runtime under planned service shutdown.

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
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("read")}, Handlers: nil}, nil)
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
	// Act: activate observation, start execution, and close the service.
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

	// Assert: observe execution failure without a runtime failure report or retained tool.
	require.ErrorIs(t, <-execution, ErrExtensionUnavailable)
	assert.Empty(t, failures)
	assert.Empty(t, service.Tools())
}

// TestServiceRemovesToolsWhenRuntimeExits handles an idle process crash without a restart.
func (s *ServiceSuite) TestServiceRemovesToolsWhenRuntimeExits() {
	t := s.T()

	// Arrange: configure one registered runtime that exits while idle.

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
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: []tool.Descriptor{testDescriptor("bash")}, Handlers: nil}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Close().Do(func() { close(closed) })
	service := New(catalog, factory, discardRuntimeFailure)
	require.Empty(t, mustLoad(t, service))
	// Act: activate process-exit observation.
	service.Activate(t.Context())

	close(done)
	<-closed

	// Assert: remove the exited runtime tools from the catalog.
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
	// Arrange: construct the Extension service suite with per-test mock setup.
	t.Parallel()

	// Act: run all service lifecycle scenarios.
	suite.Run(t, new(ServiceSuite))

	// Assert: each suite method verifies its expected catalog, result, error, and runtime state.
}

// testDescriptor creates a complete descriptor for registry behavior tests.
func testDescriptor(name string) tool.Descriptor {
	return tool.Descriptor{
		Name: name, Description: "test tool", InputSchemaJSON: []byte(`{}`),
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
	}
}

// discardProgress accepts progress when only terminal behavior matters.
func discardProgress(tool.Progress) error { return nil }

// discardRuntimeFailure keeps tests that do not exercise runtime reporting focused.
func discardRuntimeFailure(context.Context, tool.RuntimeFailure) error { return nil }
