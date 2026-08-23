//nolint:exhaustruct // Tests set only active event-union fields.
package headless

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"

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
		{Type: run.EventTextDelta, RunID: "run", Position: 1, Content: model.Content{Kind: model.ContentRefusal, Text: "I can"}},
		{Type: run.EventTextDelta, RunID: "run", Position: 1, Content: model.Content{Kind: model.ContentRefusal, Text: "not help"}},
		{Type: run.EventMessageEnd, RunID: "run", Message: model.Response{
			Content: []model.Content{{Kind: model.ContentRefusal, Text: "I cannot help"}},
		}},
	} {
		require.NoError(t, renderer.DeliverAgent(t.Context(), event))
	}

	assert.Equal(t, "I cannot help\n", stdout.String())
}

// TestRendererDoesNotWriteNewlineForToolOnlyMessage keeps stdout empty without streamed model text.
func TestRendererDoesNotWriteNewlineForToolOnlyMessage(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := NewRenderer(&stdout, &bytes.Buffer{})
	require.NoError(t, renderer.DeliverAgent(t.Context(), run.Event{
		Type: run.EventMessageEnd, RunID: "run",
		Message: model.Response{Content: []model.Content{{
			Kind:     model.ContentToolCall,
			ToolCall: model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{}},
		}}},
	}))

	assert.Empty(t, stdout.String())
}

// TestRendererSeparatesModelAndToolOutput verifies stdout remains model text only.
func TestRendererSeparatesModelAndToolOutput(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := NewRenderer(&stdout, &stderr)
	events := []run.Event{
		{Type: run.EventTextDelta, RunID: "run", Position: 0, Content: model.Content{Kind: model.ContentReasoning, Text: "hidden reasoning"}},
		{Type: run.EventTextDelta, RunID: "run", Position: 1, Content: model.Content{Kind: model.ContentText, Text: "hello"}},
		{Type: run.EventToolExecutionStart, RunID: "run", ToolCall: model.ToolCall{ID: "call", Name: "bash", Arguments: map[string]any{}}},
		{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStatus, Content: "working"}},
		{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStdout, Content: "output"}},
		{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStderr, Content: "warning"}},
		{Type: run.EventToolExecutionEnd, RunID: "run", ToolCall: model.ToolCall{ID: "call", Name: "bash", Arguments: map[string]any{}}, ToolResult: agent.ToolResult{CallID: "call", ToolName: "bash", Contents: tool.TextContents("done"), IsError: false}},
		{Type: run.EventMessageEnd, RunID: "run", Message: model.Response{
			Content:     []model.Content{{Kind: model.ContentProviderContext, ProviderContext: model.ProviderContext{ProviderID: "openai-codex", Payload: []byte("encrypted-secret")}}},
			Diagnostics: []model.Diagnostic{{Code: "provider_recovery", Message: "hidden diagnostic"}},
		}},
	}
	for _, event := range events {
		require.NoError(t, renderer.DeliverAgent(t.Context(), event))
	}
	require.NoError(t, renderer.DeliverSettled(t.Context(), "run"))

	assert.Equal(t, "hello\n", stdout.String())
	assert.Equal(t, "[tool:start] bash\n[tool:status] working\n[tool:stdout] output\n[tool:stderr] warning\n[tool:end] bash: ok\n", stderr.String())
	assert.NotContains(t, stdout.String(), "hidden reasoning")
	assert.NotContains(t, stderr.String(), "encrypted-secret")
	assert.NotContains(t, stderr.String(), "hidden diagnostic")
}

// TestRendererRendersTypedToolResultContents verifies ordered text and safe image notices.
func TestRendererRendersTypedToolResultContents(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	renderer := NewRenderer(&bytes.Buffer{}, &stderr)
	err := renderer.DeliverAgent(t.Context(), run.Event{Type: run.EventToolResult, ToolResult: agent.ToolResult{
		Contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: "first"},
			{Kind: tool.ResultContentImage, Image: tool.ResultImage{MediaType: "image/png", Data: []byte{0, 1}}},
			{Kind: tool.ResultContentText, Text: "last"},
		},
	}})
	require.NoError(t, err)
	assert.Equal(t, "[tool:result] first\n[tool:result] image omitted: image/png\n[tool:result] last\n", stderr.String())
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
		Type: run.EventTextDelta, RunID: "run", Position: 0, Content: model.Content{Kind: model.ContentText, Text: "text"},
	})

	require.Error(t, err)
}
