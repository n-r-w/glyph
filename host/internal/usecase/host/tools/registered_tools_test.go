//go:build !integration

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestServiceListsRegisteredToolsOfUnavailableExtensions verifies that both tool lists omit unavailable owners.
func TestServiceListsRegisteredToolsOfUnavailableExtensions(t *testing.T) {
	t.Parallel()
	// Arrange one available and one unavailable extension that own two tools each.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
	for _, owner := range []struct {
		id    string
		tools []startup.RawToolDescriptor
	}{
		{
			id:    "available",
			tools: []startup.RawToolDescriptor{rawDescriptor("write", "Write."), rawDescriptor("find", "Find.")},
		},
		{
			id:    "unavailable",
			tools: []startup.RawToolDescriptor{rawDescriptor("grep", "Grep."), rawDescriptor("bash", "Bash.")},
		},
	} {
		descriptors, err := service.ValidateLocal(
			startup.PendingRegistration{ID: owner.id, Path: "/" + owner.id, Tools: owner.tools, Handlers: nil},
		)
		require.NoError(t, err)
		service.Commit(
			[]startup.AcceptedRegistration{{ID: owner.id, Path: "/" + owner.id, Tools: descriptors, Handlers: nil}},
		)
	}
	runtime.EXPECT().ToolRuntimeAvailable("available").Return(true).Times(2)
	runtime.EXPECT().ToolRuntimeAvailable("unavailable").Return(false).Times(2)

	// Act by reading the catalog and the model-visible list.
	registered := service.RegisteredTools()
	visible := service.Tools()

	// Assert that both snapshots contain only available tools, sorted by name, with one decision per owner.
	require.Len(t, registered, 2)
	names := make([]string, len(registered))
	for index, entry := range registered {
		names[index] = entry.Descriptor.Name
		assert.True(t, entry.Active, entry.Descriptor.Name)
	}
	assert.Equal(t, []string{"find", "write"}, names)
	assert.Equal(t, "available", registered[0].ExtensionID)
	assert.Equal(t, "available", registered[1].ExtensionID)
	assert.Equal(t, "Find.", registered[0].Descriptor.Description)
	require.Len(t, visible, 2)
	assert.Equal(t, []tool.Descriptor{registered[0].Descriptor, registered[1].Descriptor}, visible)
}
