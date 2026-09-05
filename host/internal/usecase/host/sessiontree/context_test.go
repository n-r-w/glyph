//go:build !integration

package sessiontree

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
)

// TestObserverReceivesSessionBoundContext verifies context identity belongs to the actual handler invocation.
func TestObserverReceivesSessionBoundContext(t *testing.T) {
	t.Parallel()

	// Arrange: bind the generated context issuer and runtime mocks to a navigation capability.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	binding := extension.Context{
		ID:                "binding",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/project",
	}
	contexts.EXPECT().IssueContext("extension").Return(binding, nil)
	service := New(nil, nil, runtime)
	service.BindContextIssuer(contexts)
	runtime.EXPECT().
		HandleHandler(gomock.Any(), "extension", "observer", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, request HandlerRequest) (HandlerResponse, error) {
			assert.Equal(t, binding, request.Context)
			return HandlerResponse{
				Request:  mo.None[RequestHandlerAction](),
				Result:   mo.None[ResultHandlerAction](),
				Observer: mo.Some(ObserverAction{}),
			}, nil
		})

	// Act: invoke one accepted post-commit observer.
	err := service.invokeObserver(
		t.Context(),
		Handler{ExtensionID: "extension", HandlerID: "observer"},
		TreeObserverInvocation{},
	)

	// Assert: the observer receives the binding without changing its ordinary acknowledgement.
	require.NoError(t, err)
}
