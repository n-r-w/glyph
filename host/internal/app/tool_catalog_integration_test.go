//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// toolCatalogRunID identifies the user request of the tool-catalog scenario.
	toolCatalogRunID = "tool-catalog-run"
	// toolCatalogOwner is the extension and the tool of the fixture that reads the catalog.
	toolCatalogOwner = "external"
	// toolCatalogArguments selects the fixture mode that reads the registered tools.
	toolCatalogArguments = `{"mode":"tools"}`
)

// TestPublicExtensionReadsRegisteredToolsDuringAgentRun verifies that a real extension reads the registered tools of
// all extensions with their active state from inside its own tool during an agent run, and that the result reaches
// the client through the tool result.
//
//nolint:paralleltest // The scenario builds extension processes and starts a Programmatic Control process.
func TestPublicExtensionReadsRegisteredToolsDuringAgentRun(t *testing.T) {
	// Arrange the bundled tools extension, the fixture extension, and a provider that calls the fixture tool.
	provider := startQueueProvider(
		t, 0,
		toolCallSSE(t, "call-1", toolCatalogOwner, toolCatalogArguments),
		finalResponseSSE,
	)
	extensions := buildToolOverrideFixture(t)
	buildExternalCommand(t, extensions, "extension", toolCatalogOwner)
	fixture := startQueueFixture(t, provider, extensions)
	defer fixture.closeOwner(t)

	// Act through one user request whose tool call reads the registered tools.
	require.NoError(t, fixture.stream.Send(userRequest(toolCatalogRunID, "list the tools")))
	observation := observeFinalizedRequest(t, fixture, toolCatalogRunID)

	// Assert that the tool result lists the tools of both extensions, all active.
	require.Empty(t, observation.code, observation.message)
	texts := toolResultTexts(observation)
	require.Len(t, texts, 1)
	catalog := new(extensionpb.GetToolsResult)
	require.NoError(t, protojson.Unmarshal([]byte(texts[0]), catalog))
	owners := make(map[string]string)
	for _, registered := range catalog.GetTools() {
		owners[registered.GetName()] = registered.GetExtensionId()
		assert.True(t, registered.GetActive(), registered.GetName())
		assert.NotEmpty(t, registered.GetDescription(), registered.GetName())
	}
	assert.Equal(t, toolCatalogOwner, owners[toolCatalogOwner])
	assert.NotEmpty(t, owners[replacedToolName])
	assert.NotEqual(t, toolCatalogOwner, owners[replacedToolName])
}
