//go:build !integration

package extension

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestToolCatalogMapsRegisteredToolsWithoutOperationGate verifies that the tool read returns the name, description,
// owner, and active state of each registered tool in catalog order, and that it needs no compaction gate, which
// this service does not bind.
func TestToolCatalogMapsRegisteredToolsWithoutOperationGate(t *testing.T) {
	t.Parallel()

	// Arrange a service that binds only the tool catalog and a catalog with an active and an inactive tool.
	controller := gomock.NewController(t)
	contexts := NewMockContextOperations(controller)
	runtime := NewMockRuntimeOperations(controller)
	catalog := NewMockToolCatalog(controller)
	expectInvocationChild(t, runtime, controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	released := false
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() { released = true }, nil)
	catalog.EXPECT().RegisteredTools().Return([]tool.RegisteredTool{
		{Descriptor: testToolDescriptor("read", "Read."), ExtensionID: "tools", Active: true},
		{Descriptor: testToolDescriptor("write", "Write."), ExtensionID: "other", Active: false},
	})
	service := New(nil, contexts, runtime, "extension", "runtime")
	service.BindTools(catalog)
	request := new(extensionpb.ExtensionRequest)
	request.SetGetTools(extensionpb.GetToolsRequest_builder{Context: extensionpb.ExtensionContextRef_builder{
		ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
	}.Build()}.Build())

	// Act: admit, run, and release one tool read.
	prepared, err := service.Prepare(t.Context(), "operation", request)
	require.NoError(t, err)
	result, err := prepared.Run(t.Context(), nil)
	require.NoError(t, err)
	prepared.Release()

	// Assert: every registered tool crosses the public contract with its owner and active state.
	require.True(t, released)
	mapped := result.GetGetTools().GetTools()
	require.Len(t, mapped, 2)
	assert.Equal(t, "read", mapped[0].GetName())
	assert.Equal(t, "Read.", mapped[0].GetDescription())
	assert.Equal(t, "tools", mapped[0].GetExtensionId())
	assert.True(t, mapped[0].GetActive())
	assert.Equal(t, "write", mapped[1].GetName())
	assert.Equal(t, "other", mapped[1].GetExtensionId())
	assert.False(t, mapped[1].GetActive())
}

// testToolDescriptor returns a descriptor that carries only a name and a description.
func testToolDescriptor(name, description string) tool.Descriptor {
	return tool.Descriptor{
		Name: name, Description: description, InputSchemaJSON: nil,
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](), ReplacesTool: false,
	}
}
