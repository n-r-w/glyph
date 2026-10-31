//go:build !integration

package headless

import (
	"bytes"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestRendererWritesFinalModelText verifies that final-text rendering writes the final message text instead of
// streamed deltas.
func TestRendererWritesFinalModelText(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		final    string
		expected string
	}{
		"replaced text":         {final: "[redacted]", expected: "[redacted]\n"},
		"text with newline":     {final: "final\n", expected: "final\n"},
		"no visible final text": {final: "", expected: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a renderer for final model text and a final message that differs from the streamed text.
			var stdout bytes.Buffer
			renderer := NewRenderer(&stdout, &bytes.Buffer{})
			renderer.EnableFinalModelText()
			end := rendererMessageEndEvent()
			end.Message = mo.Some(model.Response{
				Content: []model.Content{{
					Kind: model.ContentText, Text: mo.Some(test.final), Final: true,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}},
				Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
				Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
				ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
			})

			// Act by delivering a streamed delta and the final message.
			require.NoError(t, renderer.DeliverAgent(
				t.Context(), rendererTextDeltaEvent(0, model.ContentText, "provider text"),
			))
			require.NoError(t, renderer.DeliverAgent(t.Context(), end))

			// Assert only the final text reached standard output.
			assert.Equal(t, test.expected, stdout.String())
		})
	}
}
