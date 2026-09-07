//go:build integration

package headless

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestRendererPropagatesWriterFailure preserves a real file-write failure.
func TestRendererPropagatesWriterFailure(t *testing.T) {
	t.Parallel()

	// Arrange a closed file as the renderer output.
	closedWriter, err := os.Create(filepath.Join(t.TempDir(), "closed-output"))
	require.NoError(t, err)
	require.NoError(t, closedWriter.Close())
	renderer := NewRenderer(closedWriter, &bytes.Buffer{})

	// Act by delivering visible model text.
	err = renderer.DeliverAgent(
		t.Context(),
		agent.Event{
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[agent.TurnSummary](),
			Agent:      mo.None[agent.RunSummary](),
			Type:       agent.EventTextDelta,
			RunID:      "run",
			Position:   mo.Some(0),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some("text"),
			}),
		},
	)

	// Assert the filesystem write failure reaches the caller.
	require.Error(t, err)
}

// TestRendererReportIssuePreservesSourceAndWriterFailure verifies failed startup diagnostic output.
func TestRendererReportIssuePreservesSourceAndWriterFailure(t *testing.T) {
	t.Parallel()

	// Arrange a source issue and a closed stderr file.
	closedWriter, err := os.Create(filepath.Join(t.TempDir(), "closed-stderr"))
	require.NoError(t, err)
	require.NoError(t, closedWriter.Close())
	renderer := NewRenderer(&bytes.Buffer{}, closedWriter)
	source := errors.New("complete extension load failure suffix")
	issue := startup.Issue{PluginIDs: []string{"broken"}, Path: "/broken", Err: source}

	// Act through the headless startup reporter.
	err = renderer.ReportIssue(t.Context(), issue)

	// Assert both causes remain accessible after the diagnostic write fails.
	require.ErrorIs(t, err, source)
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorContains(t, err, source.Error())
}
