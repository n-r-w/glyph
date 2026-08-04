//nolint:exhaustruct // Tests set only active event-union fields.
package headless

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// TestRendererReportsRuntimeFailure writes one classified identity-bearing failure to stderr.
func TestRendererReportsRuntimeFailure(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := NewRenderer(&stdout, &stderr)

	err := renderer.ReportRuntimeFailure(t.Context(), tool.RuntimeFailure{
		PluginID: "crashed-plugin", Condition: tool.RuntimeUnavailableProcessExited,
	})

	require.NoError(t, err)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "[extension:error] extension crashed-plugin unavailable: extension process exited\n", stderr.String())
}

// TestRendererPrintsRefusalDeltasOnce verifies message finalization does not repeat streamed refusal text.
func TestRendererPrintsRefusalDeltasOnce(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := NewRenderer(&stdout, &bytes.Buffer{})
	for _, event := range []run.Event{
		{Type: run.EventMessageUpdate, RunID: "run", Position: 1, Delta: "I can"},
		{Type: run.EventMessageUpdate, RunID: "run", Position: 1, Delta: "not help"},
		{Type: run.EventMessageEnd, RunID: "run", Message: agent.ModelResponse{
			Items: []agent.ModelItem{{Kind: agent.ModelItemText, Text: "I cannot help"}},
		}},
	} {
		require.NoError(t, renderer.DeliverAgent(t.Context(), event))
	}

	assert.Equal(t, "I cannot help", stdout.String())
}

// TestRendererSeparatesModelAndToolOutput verifies stdout remains model text only.
func TestRendererSeparatesModelAndToolOutput(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := NewRenderer(&stdout, &stderr)
	events := []run.Event{
		{Type: run.EventMessageUpdate, RunID: "run", Position: 0, Delta: "hello"},
		{Type: run.EventToolExecutionStart, RunID: "run", ToolCall: agent.ToolCall{ID: "call", Name: "bash", Arguments: map[string]any{}}},
		{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStatus, Content: "working"}},
		{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStdout, Content: "output"}},
		{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStderr, Content: "warning"}},
		{Type: run.EventToolExecutionEnd, RunID: "run", ToolCall: agent.ToolCall{ID: "call", Name: "bash", Arguments: map[string]any{}}, ToolResult: agent.ToolResult{CallID: "call", ToolName: "bash", Content: "done", IsError: false}},
		{Type: run.EventMessageEnd, RunID: "run", Message: agent.ModelResponse{Items: []agent.ModelItem{{Kind: agent.ModelItemProviderContext, ProviderContext: agent.ProviderContext{ProviderID: "openai-codex", Payload: []byte("encrypted-secret")}}}}},
	}
	for _, event := range events {
		require.NoError(t, renderer.DeliverAgent(t.Context(), event))
	}
	require.NoError(t, renderer.DeliverSettled(t.Context(), "run"))

	assert.Equal(t, "hello", stdout.String())
	assert.Equal(t, "[tool:start] bash\n[tool:status] working\n[tool:stdout] output\n[tool:stderr] warning\n[tool:end] bash: ok\n", stderr.String())
	assert.NotContains(t, stderr.String(), "encrypted-secret")
}

// TestRendererWritesStartupInformationAndFailures verifies distinct informational and failure prefixes.
func TestRendererWritesStartupInformationAndFailures(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	renderer := NewRenderer(&bytes.Buffer{}, &stderr)
	issue := toolservice.Issue{PluginIDs: []string{"broken"}, Path: "/plugins/broken", Err: errors.New("handshake failed")}
	report := toolservice.LoadReport{Issues: []toolservice.Issue{issue}, Extensions: []toolservice.LoadedExtension{{
		ID: "tools", Tools: []tool.Descriptor{{Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`)}, {Name: "bash", Description: "bash", InputSchemaJSON: []byte(`{}`)}},
	}}}

	require.NoError(t, renderer.ReportIssue(t.Context(), issue))
	require.NoError(t, renderer.ReportSummary(t.Context(), report))
	require.NoError(t, renderer.WriteError(errors.New("provider failed")))

	assert.Equal(t, "[extension:error] broken (/plugins/broken): handshake failed\n[info] headless\n[info] extension tools: read, bash\n[error] provider failed\n", stderr.String())
}

// TestRendererReportsEmptyExtensionCatalogAsInformation verifies empty startup is not a warning.
func TestRendererReportsEmptyExtensionCatalogAsInformation(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	renderer := NewRenderer(&bytes.Buffer{}, &stderr)

	require.NoError(t, renderer.ReportSummary(t.Context(), toolservice.LoadReport{Issues: nil, Extensions: nil}))

	assert.Equal(t, "[info] headless\n[info] extensions: none\n", stderr.String())
}

// TestRendererPropagatesWriterFailure verifies synchronous delivery has no retry.
func TestRendererPropagatesWriterFailure(t *testing.T) {
	t.Parallel()

	closedWriter, err := os.Create(filepath.Join(t.TempDir(), "closed-output"))
	require.NoError(t, err)
	require.NoError(t, closedWriter.Close())
	renderer := NewRenderer(closedWriter, &bytes.Buffer{})

	err = renderer.DeliverAgent(t.Context(), run.Event{
		Type: run.EventMessageUpdate, RunID: "run", Position: 0, Delta: "text",
	})

	require.Error(t, err)
}
