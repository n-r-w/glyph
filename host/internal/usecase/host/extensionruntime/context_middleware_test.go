//go:build !integration

package extensionruntime

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
)

// TestContextMiddlewareChecksExactRuntimeGeneration forwards instruction and history handlers only to the bound runtime.
func TestContextMiddlewareChecksExactRuntimeGeneration(t *testing.T) {
	t.Parallel()
	// Arrange one live runtime generation and bindings for the current and a previous generation.
	mocks := gomock.NewController(t)
	process := NewMockExtensionRuntime(mocks)
	service := New(nil, nil, nil)
	service.runtimes["extension"] = &runtimeState{
		runtime: process, instanceID: "current", available: true, activeExecutions: 0, exitPending: false,
		work: sync.WaitGroup{}, closeOnce: sync.Once{}, shutdownDone: make(chan struct{}), observed: false,
		monitorDone: make(chan struct{}), commit: sync.RWMutex{}, invalidated: make(chan struct{}),
		invalidateOnce: sync.Once{},
	}
	current := extension.Context{
		ID: "binding", ExtensionID: "extension", RuntimeInstanceID: "current", SessionID: "", WorkingDirectory: "",
	}
	previous := current
	previous.RuntimeInstanceID = "previous"
	instructions := middleware.Transformation[string]{Context: current, Original: "base", Current: "base"}
	history := middleware.Transformation[[]agent.HistoryEntry]{Context: current, Original: nil, Current: nil}
	process.EXPECT().HandleSystemInstructions(gomock.Any(), "instructions", instructions).Return(
		middleware.Transformed[string]{Kind: middleware.Replace, Replacement: "changed"}, nil,
	)
	process.EXPECT().HandleModelContext(gomock.Any(), "context", history).Return(
		middleware.Transformed[[]agent.HistoryEntry]{Kind: middleware.Preserve, Replacement: nil}, nil,
	)

	// Act through the current and the stale generation.
	instructionsAction, instructionsErr := service.HandleSystemInstructions(
		t.Context(), "extension", "instructions", instructions,
	)
	historyAction, historyErr := service.HandleModelContext(t.Context(), "extension", "context", history)
	instructions.Context = previous
	history.Context = previous
	_, staleInstructionsErr := service.HandleSystemInstructions(t.Context(), "extension", "instructions", instructions)
	_, staleHistoryErr := service.HandleModelContext(t.Context(), "extension", "context", history)

	// Assert only the exact generation is invoked; strict mocks reject stale invocations.
	require.NoError(t, instructionsErr)
	require.Equal(
		t,
		middleware.Transformed[string]{Kind: middleware.Replace, Replacement: "changed"},
		instructionsAction,
	)
	require.NoError(t, historyErr)
	require.Equal(t, middleware.Preserve, historyAction.Kind)
	require.ErrorIs(t, staleInstructionsErr, ErrExtensionUnavailable)
	require.ErrorIs(t, staleHistoryErr, ErrExtensionUnavailable)
}
