package headless

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// rendererTextDeltaEvent creates one visible model text delta.
func rendererTextDeltaEvent(position int, kind model.ContentKind, text string) run.Event {
	return run.Event{
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
		Type:       run.EventTextDelta,
		RunID:      "run",
		Position:   mo.Some(position),
		Content: mo.Some(model.Content{
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
			Kind:            kind,
			Text:            mo.Some(text),
		}),
	}
}

// rendererMessageEndEvent creates one model message finalization event.
func rendererMessageEndEvent() run.Event {
	return run.Event{
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
		Message:    mo.None[model.Response](),
		Type:       run.EventMessageEnd,
		RunID:      "run",
	}
}

// rendererProgressEvent creates one tool progress event.
func rendererProgressEvent(channel tool.ProgressChannel, content string) run.Event {
	return run.Event{
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
		Type:       run.EventToolExecutionUpdate,
		RunID:      "run",
		Progress:   mo.Some(tool.Progress{Channel: channel, Content: content}),
	}
}

// TestRendererReportsRuntimeFailure writes one classified identity-bearing failure to stderr.
func TestRendererReportsRuntimeFailure(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := NewRenderer(&stdout, &stderr)

	err := renderer.ReportRuntimeFailure(t.Context(), tool.RuntimeFailure{
		PluginID:  "crashed-plugin",
		Condition: tool.RuntimeUnavailableProcessExited,
	})

	require.NoError(t, err)
	assert.Empty(t, stdout.String())
	assert.Equal(
		t,
		"[extension:error] extension crashed-plugin unavailable: extension process exited\n",
		stderr.String(),
	)
}

// TestRendererPrintsRefusalDeltasOnce verifies message finalization does not repeat streamed refusal text.
func TestRendererPrintsRefusalDeltasOnce(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := NewRenderer(&stdout, &bytes.Buffer{})
	for _, event := range []run.Event{
		rendererTextDeltaEvent(1, model.ContentRefusal, "I can"),
		rendererTextDeltaEvent(1, model.ContentRefusal, "not help"),
		{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventMessageEnd,
			RunID:      "run",
			Message: mo.Some(model.Response{
				Outcome:       mo.None[model.Outcome](),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
				Content: []model.Content{{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentRefusal,
					Text:            mo.Some("I cannot help"),
				}},
			}),
		},
	} {
		require.NoError(t, renderer.DeliverAgent(t.Context(), event))
	}

	assert.Equal(t, "I cannot help\n", stdout.String())
}

// TestRendererFinalizesVisibleTextWithOneTrailingNewline verifies byte-exact message termination.
func TestRendererFinalizesVisibleTextWithOneTrailingNewline(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		chunks   []string
		expected string
	}{
		"no visible text":            {chunks: nil, expected: ""},
		"open line":                  {chunks: []string{"hello"}, expected: "hello\n"},
		"closed line":                {chunks: []string{"hello\n"}, expected: "hello\n"},
		"empty delta preserves line": {chunks: []string{"hello", ""}, expected: "hello\n"},
		"last chunk closes line":     {chunks: []string{"hello", " world\n"}, expected: "hello world\n"},
		"last chunk reopens line":    {chunks: []string{"hello\n", "world"}, expected: "hello\nworld\n"},
		"multiple trailing newlines": {chunks: []string{"hello\n\n"}, expected: "hello\n\n"},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			renderer := NewRenderer(&stdout, &bytes.Buffer{})
			for _, chunk := range testCase.chunks {
				require.NoError(t, renderer.DeliverAgent(t.Context(), rendererTextDeltaEvent(0, model.ContentText, chunk)))
			}
			require.NoError(t, renderer.DeliverAgent(t.Context(), rendererMessageEndEvent()))

			assert.Equal(t, testCase.expected, stdout.String())
		})
	}
}

// TestRendererDoesNotWriteNewlineForToolOnlyMessage keeps stdout empty without streamed model text.
func TestRendererDoesNotWriteNewlineForToolOnlyMessage(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := NewRenderer(&stdout, &bytes.Buffer{})
	require.NoError(
		t,
		renderer.DeliverAgent(t.Context(), run.Event{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventMessageEnd,
			RunID:      "run",
			Message: mo.Some(model.Response{
				Outcome:       mo.None[model.Outcome](),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
				Content: []model.Content{
					{
						Text:            mo.None[string](),
						Final:           false,
						ProviderContext: mo.None[model.ProviderContext](),
						Kind:            model.ContentToolCall,
						ToolCall: mo.Some(
							model.ToolCall{
								ID:        "call",
								Name:      "read",
								Arguments: map[string]any{},
							},
						),
					},
				},
			}),
		}),
	)

	assert.Empty(t, stdout.String())
}

// TestRendererSeparatesModelAndToolOutput verifies stdout remains model text only.
func TestRendererSeparatesModelAndToolOutput(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := NewRenderer(&stdout, &stderr)
	events := []run.Event{
		rendererTextDeltaEvent(0, model.ContentReasoning, "hidden reasoning"),
		rendererTextDeltaEvent(1, model.ContentText, "hello"),
		{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventToolExecutionStart,
			RunID:      "run",
			ToolCall: mo.Some(model.ToolCall{
				ID:        "call",
				Name:      "bash",
				Arguments: map[string]any{},
			}),
		},
		rendererProgressEvent(tool.ProgressChannelStatus, "working"),
		rendererProgressEvent(tool.ProgressChannelStdout, "output"),
		rendererProgressEvent(tool.ProgressChannelStderr, "warning"),
		{
			Position: mo.None[int](),
			Content:  mo.None[model.Content](),
			Message:  mo.None[model.Response](),
			Preview:  mo.None[model.ToolCallPreview](),
			Progress: mo.None[tool.Progress](),
			Turn:     mo.None[run.TurnSummary](),
			Agent:    mo.None[run.AgentSummary](),
			Type:     run.EventToolExecutionEnd,
			RunID:    "run",
			ToolCall: mo.Some(model.ToolCall{
				ID:        "call",
				Name:      "bash",
				Arguments: map[string]any{},
			}),
			ToolResult: mo.Some(agent.ToolResult{
				CallID:   "call",
				ToolName: "bash",
				Contents: tool.TextContents("done"),
				IsError:  false,
			}),
		},
		{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventMessageEnd,
			RunID:      "run",
			Message: mo.Some(model.Response{
				Outcome:       mo.None[model.Outcome](),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Content: []model.Content{
					{
						Text:     mo.None[string](),
						Final:    false,
						ToolCall: mo.None[model.ToolCall](),
						Kind:     model.ContentReasoning,
						ProviderContext: mo.Some(
							model.ProviderContext{
								Source: model.ProviderContextSource{
									API:              "",
									Model:            "",
									CompatibilityKey: mo.None[string](),
									ProviderID:       "openai-codex",
								},
								Payload: []byte("encrypted-secret"),
							},
						),
					},
				},
				Diagnostics: []model.Diagnostic{
					{
						Code:    "provider_recovery",
						Message: "hidden diagnostic",
					},
				},
			}),
		},
	}
	for _, event := range events {
		require.NoError(t, renderer.DeliverAgent(t.Context(), event))
	}
	require.NoError(t, renderer.DeliverSettled(t.Context(), "run"))

	assert.Equal(t, "hello\n", stdout.String())
	assert.Equal(
		t,
		"[tool:start] bash\n[tool:status] working\n[tool:stdout] output\n[tool:stderr] warning\n[tool:end] bash: ok\n",
		stderr.String(),
	)
	assert.NotContains(t, stdout.String(), "hidden reasoning")
	assert.NotContains(t, stderr.String(), "encrypted-secret")
	assert.NotContains(t, stderr.String(), "hidden diagnostic")
}

// TestRendererRendersTypedToolResultContents verifies ordered text and safe image notices.
func TestRendererRendersTypedToolResultContents(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	renderer := NewRenderer(&bytes.Buffer{}, &stderr)
	err := renderer.DeliverAgent(
		t.Context(),
		run.Event{
			RunID:    "",
			Position: mo.None[int](),
			Content:  mo.None[model.Content](),
			Message:  mo.None[model.Response](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Progress: mo.None[tool.Progress](),
			Turn:     mo.None[run.TurnSummary](),
			Agent:    mo.None[run.AgentSummary](),
			Type:     run.EventToolResult,
			ToolResult: mo.Some(agent.ToolResult{
				CallID:   "",
				ToolName: "",
				IsError:  false,
				Contents: []tool.ResultContent{
					{
						Kind:  tool.ResultContentText,
						Text:  mo.Some("first"),
						Image: mo.None[tool.ResultImage](),
					},
					{
						Kind: tool.ResultContentImage,
						Text: mo.None[string](),
						Image: mo.Some(
							tool.ResultImage{
								MediaType: "image/png",
								Data:      []byte{0, 1},
							},
						),
					},
					{
						Kind:  tool.ResultContentText,
						Text:  mo.Some("last"),
						Image: mo.None[tool.ResultImage](),
					},
				},
			}),
		},
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		"[tool:result] first\n[tool:result] image omitted: image/png\n[tool:result] last\n",
		stderr.String(),
	)
}

// TestRendererWritesStartupInformationAndFailures verifies distinct informational and failure prefixes.
func TestRendererWritesStartupInformationAndFailures(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	renderer := NewRenderer(&bytes.Buffer{}, &stderr)
	issue := toolservice.Issue{
		PluginIDs: []string{"broken"},
		Path:      "/plugins/broken",
		Err:       errors.New("handshake failed"),
	}
	report := toolservice.LoadReport{
		Issues: []toolservice.Issue{issue},
		Extensions: []toolservice.LoadedExtension{
			{
				Path: "",
				ID:   "tools",
				Tools: []tool.Descriptor{
					{
						ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
						Name:                "read",
						Description:         "read",
						InputSchemaJSON:     []byte(`{}`),
					},
					{
						ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
						Name:                "bash",
						Description:         "bash",
						InputSchemaJSON:     []byte(`{}`),
					},
				},
			},
		},
	}

	require.NoError(t, renderer.ReportIssue(t.Context(), issue))
	require.NoError(t, renderer.ReportSummary(t.Context(), report))
	require.NoError(t, renderer.WriteError(errors.New("provider failed")))

	assert.Equal(
		t,
		"[extension:error] broken (/plugins/broken): handshake failed\n[info] headless\n[info] extension tools: read, bash\n[error] provider failed\n",
		stderr.String(),
	)
}

// TestRendererReportsEmptyExtensionCatalogAsInformation verifies empty startup is not a warning.
func TestRendererReportsEmptyExtensionCatalogAsInformation(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	renderer := NewRenderer(&bytes.Buffer{}, &stderr)

	require.NoError(t, renderer.ReportSummary(t.Context(), toolservice.LoadReport{}))

	assert.Equal(t, "[info] headless\n[info] extensions: none\n", stderr.String())
}

// TestRendererPropagatesWriterFailure verifies synchronous delivery has no retry.
func TestRendererPropagatesWriterFailure(t *testing.T) {
	t.Parallel()

	closedWriter, err := os.Create(filepath.Join(t.TempDir(), "closed-output"))
	require.NoError(t, err)
	require.NoError(t, closedWriter.Close())
	renderer := NewRenderer(closedWriter, &bytes.Buffer{})

	err = renderer.DeliverAgent(
		t.Context(),
		run.Event{
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventTextDelta,
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

	require.Error(t, err)
}

// TestRendererRejectsMissingSelectedPayload verifies malformed events do not render zero values.
func TestRendererRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	renderer := NewRenderer(&bytes.Buffer{}, &bytes.Buffer{})
	err := renderer.DeliverAgent(t.Context(), run.Event{
		Type:       run.EventToolResult,
		RunID:      "run",
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
	})

	require.ErrorContains(t, err, "requires tool result")
}
