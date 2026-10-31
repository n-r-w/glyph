//go:build !integration

package startup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestServiceLoadPassesContextMiddlewareToRegistrar accepts instruction and context handlers with wire handlers.
func TestServiceLoadPassesContextMiddlewareToRegistrar(t *testing.T) {
	t.Parallel()

	// Arrange one registration that mixes middleware kinds with a lifecycle observer.
	controller := gomock.NewController(t)
	runtimes := NewMockRuntimeLoader(controller)
	tools := NewMockToolRegistrar(controller)
	sessionTree := NewMockSessionTreeRegistrar(controller)
	lifecycle := NewMockLifecycleRegistrar(controller)
	middleware := NewMockMiddlewareRegistrar(controller)
	pending := PendingRegistration{
		ID: "extension", Path: "/extension", Tools: nil,
		Handlers: []RawHandlerDescriptor{
			{Present: true, ID: "context", Kind: RawHandlerKindModelContext},
			{Present: true, ID: "agent", Kind: RawHandlerKindAgentStart},
			{Present: true, ID: "instructions", Kind: RawHandlerKindSystemInstructions},
			{Present: true, ID: "headers", Kind: RawHandlerKindProviderHeaders},
			{Present: true, ID: "input", Kind: RawHandlerKindUserInput},
		},
	}
	middlewareHandlers := []AcceptedHandler{
		{ID: "context", Kind: RawHandlerKindModelContext},
		{ID: "instructions", Kind: RawHandlerKindSystemInstructions},
		{ID: "headers", Kind: RawHandlerKindProviderHeaders},
		{ID: "input", Kind: RawHandlerKindUserInput},
	}
	runtimes.EXPECT().
		LoadPending(t.Context(), gomock.Any()).
		Return(PendingLoad{Issues: nil, Registrations: []PendingRegistration{pending}}, nil)
	tools.EXPECT().ValidateLocal(pending).Return([]tool.Descriptor{}, nil)
	sessionTree.EXPECT().
		ValidateSessionTreeHandlers(gomock.Any()).
		Return([]AcceptedHandler{}, nil)
	lifecycle.EXPECT().
		ValidateLifecycleHandlers(gomock.Any()).
		Return([]AcceptedHandler{{ID: "agent", Kind: RawHandlerKindAgentStart}}, nil)
	middleware.EXPECT().ValidateMiddlewareHandlers(PendingRegistration{
		ID: "extension", Path: "/extension", Tools: nil,
		Handlers: []RawHandlerDescriptor{
			{Present: true, ID: "context", Kind: RawHandlerKindModelContext},
			{Present: true, ID: "instructions", Kind: RawHandlerKindSystemInstructions},
			{Present: true, ID: "headers", Kind: RawHandlerKindProviderHeaders},
			{Present: true, ID: "input", Kind: RawHandlerKindUserInput},
		},
	}).Return(middlewareHandlers, nil)
	expected := []AcceptedRegistration{{
		ID: "extension", Path: "/extension", Tools: []tool.Descriptor{},
		Handlers: []AcceptedHandler{
			middlewareHandlers[0],
			{ID: "agent", Kind: RawHandlerKindAgentStart},
			middlewareHandlers[1],
			middlewareHandlers[2],
			middlewareHandlers[3],
		},
	}}
	tools.EXPECT().Conflicts(expected).Return(nil)
	runtimes.EXPECT().RejectPending([]string{})
	tools.EXPECT().Commit(expected)
	sessionTree.EXPECT().CommitSessionTreeHandlers(expected)
	lifecycle.EXPECT().CommitLifecycleHandlers(expected)
	middleware.EXPECT().CommitMiddlewareHandlers(expected)
	runtimes.EXPECT().Accept(expected)
	service := New(runtimes, tools, sessionTree, lifecycle, nil)
	require.NoError(t, service.BindMiddleware(middleware))

	// Act by loading the mixed registration.
	report, err := service.Load(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: "/plugins"})

	// Assert the middleware owner received every middleware kind and registration order is unchanged.
	require.NoError(t, err)
	assert.Empty(t, report.Issues)
	assert.Equal(t, expected, report.Extensions)
}
