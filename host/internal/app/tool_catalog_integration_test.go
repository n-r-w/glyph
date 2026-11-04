//go:build integration

package app

import (
	"encoding/json/v2"
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
	// activeToolsSteer asks the real extension input handler to replace the active set.
	activeToolsSteer = "select only external tool"
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

// TestPublicExtensionSetsActiveToolsDuringModelRequest verifies snapshot isolation through a real extension handler.
//
//nolint:paralleltest // The scenario builds extension processes and starts a Programmatic Control process.
func TestPublicExtensionSetsActiveToolsDuringModelRequest(t *testing.T) {
	// Arrange a held model request and an extension with a tool-selection input handler.
	provider := startQueueProvider(
		t, 1, toolCallSSE(t, "call-1", toolCatalogOwner, toolCatalogArguments), finalResponseSSE,
	)
	extensions := buildToolOverrideFixture(t)
	buildExternalCommand(t, extensions, "extension", toolCatalogOwner)
	fixture := startQueueFixture(t, provider, extensions)
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(toolCatalogRunID, "inspect tool selection")))
	<-provider.held[0]
	before := provider.snapshot()[0]
	// Act through steer input while the first request remains held at the provider.
	queued := sendSteer(t, fixture, "select-tools", activeToolsSteer)
	requireQueued(t, queued)
	assert.Equal(t, before, provider.snapshot()[0])
	close(provider.release[0])
	observation := observeFinalizedRequest(t, fixture, toolCatalogRunID)
	// Assert the requests keep separate tool snapshots and the catalog retains disabled tools.
	require.Empty(t, observation.code, observation.message)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	assert.Contains(t, requestToolNames(t, bodies[0]), replacedToolName)
	assert.Equal(t, []string{toolCatalogOwner}, requestToolNames(t, bodies[1]))
	texts := toolResultTexts(observation)
	require.Len(t, texts, 1)
	catalog := new(extensionpb.GetToolsResult)
	require.NoError(t, protojson.Unmarshal([]byte(texts[0]), catalog))
	require.Greater(t, len(catalog.GetTools()), 1)
	for _, entry := range catalog.GetTools() {
		assert.Equal(t, entry.GetName() == toolCatalogOwner, entry.GetActive(), entry.GetName())
	}
}

// requestToolNames extracts the model-visible tool names from a provider request.
func requestToolNames(t *testing.T, body string) []string {
	t.Helper()
	var request struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &request))
	names := make([]string, 0, len(request.Tools))
	for _, descriptor := range request.Tools {
		names = append(names, descriptor.Name)
	}
	return names
}

// TestPublicExtensionSetsActiveToolsInsideOwnTool verifies that a nested update completes without the operation gate.
//
//nolint:paralleltest // The scenario builds extension processes and starts a Programmatic Control process.
func TestPublicExtensionSetsActiveToolsInsideOwnTool(t *testing.T) {
	// Arrange a provider that invokes the selection tool while an agent run owns admission.
	provider := startQueueProvider(
		t, 0, toolCallSSE(t, "call-1", toolCatalogOwner, `{"mode":"set-tools"}`), finalResponseSSE,
	)
	extensions := buildToolOverrideFixture(t)
	buildExternalCommand(t, extensions, "extension", toolCatalogOwner)
	fixture := startQueueFixture(t, provider, extensions)
	defer fixture.closeOwner(t)
	// Act through Programmatic Control and the real extension's public context API.
	require.NoError(t, fixture.stream.Send(userRequest(toolCatalogRunID, "select tools")))
	observation := observeFinalizedRequest(t, fixture, toolCatalogRunID)
	// Assert the nested operation completes and the following request uses the selection.
	require.Empty(t, observation.code, observation.message)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	assert.Contains(t, requestToolNames(t, bodies[0]), replacedToolName)
	assert.Equal(t, []string{toolCatalogOwner}, requestToolNames(t, bodies[1]))
}
