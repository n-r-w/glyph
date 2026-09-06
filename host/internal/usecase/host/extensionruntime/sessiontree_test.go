//go:build !integration

package extensionruntime

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestServiceInvokesSessionTreeHandler verifies low-level invocation preserves runtime accounting and payloads.
func TestServiceInvokesSessionTreeHandler(t *testing.T) {
	t.Parallel()

	// Arrange one accepted runtime and one session-tree-owned handler payload.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins"}).Return(Discovery{
		DirectoryError: nil,
		Candidates: []Executable{{
			ID: "tree", Path: "/tree",
		}}, Issues: nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().Register(t.Context()).Return(Registration{
		Tools: nil, Handlers: nil,
	}, nil)
	service := New(catalog, factory, newRuntimeReporter(t, nil))
	pending, err := service.LoadPending(t.Context(), startup.Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	accepted := []startup.AcceptedRegistration{{ID: "tree", Path: "/tree", Tools: nil, Handlers: nil}}
	service.Accept(accepted)
	request := sessiontree.HandlerRequest{
		Context:  runtimeBindingForTest(service, "tree"),
		Request:  mo.Some(sessiontree.RequestHandlerInvocation{}),
		Result:   mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.None[sessiontree.TreeObserverInvocation](),
	}
	response := sessiontree.HandlerResponse{
		Request: mo.Some(sessiontree.RequestHandlerAction{}),
		Result:  mo.None[sessiontree.ResultHandlerAction](), Observer: mo.None[sessiontree.ObserverAction](),
	}
	runtime.EXPECT().
		Handle(t.Context(), "request", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, payload HandlerInvocation) (HandlerAction, error) {
			assert.Equal(t, request.Context, payload.Context)
			assert.Equal(t, InvocationRequest, payload.Kind)
			return HandlerAction{
				Kind:          InvocationRequest,
				Cancel:        false,
				RequestAction: 0,
				Request:       mo.None[Navigation](),
				ResultAction:  0,
				Result:        mo.None[Summary](),
			}, nil
		})

	// Act through the session-tree-owned runtime interface.
	available := service.HandlerRuntimeAvailable(pending.Registrations[0].ID)
	actual, err := service.HandleHandler(t.Context(), "tree", "request", request)

	// Assert runtime availability and the low-level response are unchanged.
	require.NoError(t, err)
	assert.True(t, available)
	assert.Equal(t, response, actual)
	runtime.EXPECT().Close()
	service.Close()
}
