//go:build !integration

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestActiveToolsReplaceAtomically verifies selection, unavailable owners, rejection, and reactivation.
func TestActiveToolsReplaceAtomically(t *testing.T) {
	t.Parallel()
	// Arrange two tools owned by an available extension and one by an unavailable extension.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
	for _, id := range []string{"available", "unavailable"} {
		raw := []startup.RawToolDescriptor{rawDescriptor("read", "Read."), rawDescriptor("write", "Write.")}
		if id == "unavailable" {
			raw = []startup.RawToolDescriptor{rawDescriptor("hidden", "Hidden.")}
		}
		descriptors, err := service.ValidateLocal(
			startup.PendingRegistration{ID: id, Path: "/" + id, Tools: raw, Handlers: nil},
		)
		require.NoError(t, err)
		service.Commit([]startup.AcceptedRegistration{{ID: id, Path: "/" + id, Tools: descriptors, Handlers: nil}})
	}
	runtime.EXPECT().ToolRuntimeAvailable("available").Return(true).AnyTimes()
	runtime.EXPECT().ToolRuntimeAvailable("unavailable").Return(false).AnyTimes()
	original := service.Tools()
	require.Len(t, original, 2)
	// Act by selecting a known unavailable tool as well as one available tool.
	require.NoError(t, service.SetActiveTools([]string{"read", "hidden", "read"}))
	registered := service.RegisteredTools()
	selected := service.Tools()
	result, err := service.Execute(
		t.Context(),
		model.ToolCall{ID: "call", Name: "write", Arguments: testToolCallArguments(`{}`)},
		nil,
	)
	// Assert omitted available tools remain in the catalog but cannot run, and old snapshots remain unchanged.
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, agent.ToolErrorUnavailableTool, result.ErrorCategory.MustGet())
	require.Len(t, registered, 2)
	assert.True(t, registered[0].Active)
	assert.False(t, registered[1].Active)
	require.Len(t, selected, 1)
	assert.Equal(t, "read", selected[0].Name)
	assert.Len(t, original, 2)
	require.ErrorContains(t, service.SetActiveTools([]string{"write", "unknown"}), "unknown")
	assert.Equal(t, selected, service.Tools())
	require.NoError(t, service.SetActiveTools(nil))
	assert.Empty(t, service.Tools())
	require.Len(t, service.RegisteredTools(), 2)
	require.NoError(t, service.SetActiveTools([]string{"read", "write"}))
	assert.Equal(t, original, service.Tools())
}
