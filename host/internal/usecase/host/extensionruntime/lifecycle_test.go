//go:build !integration

package extensionruntime

import (
	"context"
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestServiceLifecycleRuntimeFailureDisablesOnlyOwner verifies lifecycle invocation reuses runtime failure ownership.
func TestServiceLifecycleRuntimeFailureDisablesOnlyOwner(t *testing.T) {
	t.Parallel()

	// Arrange one accepted runtime that fails its lifecycle operation as unavailable.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins"}).Return(Discovery{
		DirectoryError: nil,
		Candidates:     []Executable{{ID: "observer", Path: "/observer"}}, Issues: nil,
	}, nil)
	factory.EXPECT().Start(t.Context(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().
		Register(t.Context()).
		Return(Registration{Tools: nil, Handlers: nil}, nil)
	reported := make(chan extension.RuntimeFailure, 1)
	service := New(
		catalog,
		factory,
		newRuntimeReporter(t, func(_ context.Context, failure extension.RuntimeFailure) error {
			reported <- failure
			return nil
		}),
	)
	pending, err := service.LoadPending(t.Context(), startup.Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	service.Accept([]startup.AcceptedRegistration{{ID: "observer", Path: "/observer", Tools: nil, Handlers: nil}})
	binding := runtimeBindingForTest(service, "observer")
	failure := fmt.Errorf("%w: process exited: exact cause", ErrExtensionUnavailable)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "handler", gomock.Any()).Return(failure)
	runtime.EXPECT().Close()

	// Act through the lifecycle-owned runtime interface.
	unavailable, err := service.ObserveLifecycle(
		t.Context(), pending.Registrations[0].ID, "handler", binding,
		lifecycle.Event{Agent: lifecycleEventForRuntime(), Settled: false},
	)

	// Assert the owner reports and disables this runtime while preserving the complete cause.
	require.ErrorIs(t, err, failure)
	assert.True(t, unavailable)
	assert.False(t, service.HandlerRuntimeAvailable("observer"))
	assert.Equal(t, "observer", (<-reported).PluginID)
	service.Close()
}

// lifecycleEventForRuntime creates the minimal transport-neutral invocation payload.
func lifecycleEventForRuntime() agent.Event {
	return agent.Event{
		Type:       agent.EventAgentStart,
		RunID:      "run",
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
	}
}
