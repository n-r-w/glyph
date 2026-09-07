//go:build !integration

package terminal

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelRendersWarningAndExtensionIdentityPath preserves startup severity and identity text.
func TestModelRendersWarningAndExtensionIdentityPath(t *testing.T) {
	t.Parallel()
	// Arrange startup diagnostics in a display snapshot.
	model := newRenderModel()
	model.snapshot.Body.Startup = []presentation.Line{
		renderTextLine(presentation.LineWarning, "excluded UI optional at /plugins/ui/optional"),
		renderTextLine(
			presentation.LineInformation,
			"UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read",
		),
	}
	// Act by rendering startup data.
	view := model.View().Content
	// Assert severity, path, and once-only display are retained.
	assert.Contains(t, view, "[warning] excluded UI optional at /plugins/ui/optional")
	assert.Contains(t, view, "[info] UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read")
	assert.Equal(t, 1, strings.Count(view, "glyph-tools at /plugins/extension/glyph-tools"))
}

// TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize preserves view composition and dimensions.
func TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize(t *testing.T) {
	t.Parallel()
	// Arrange a coherent snapshot with every view section populated.
	model := newRenderModel()
	model.snapshot.Body.Startup = []presentation.Line{
		renderTextLine(presentation.LineInformation, "Glyph session initialized."),
	}
	model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineInformation, "Ready.")}
	model.snapshot.Body.ActiveModel = map[int]presentation.ActiveModelContent{
		1: {Kind: mo.None[presentation.ModelContentKind](), Text: mo.Some("Working")},
	}
	model.snapshot.Body.AuthorizationURL = mo.Some("https://example.test/oauth")
	model.snapshot.Input, model.snapshot.Cursor = []rune("hello"), 5
	// Act by applying only the framework-owned window resize and rendering.
	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	view := model.View()
	// Assert output includes every published section without an application transition.
	assert.True(t, view.AltScreen)
	assert.Contains(t, view.Content, "Glyph session initialized.")
	assert.Contains(t, view.Content, "[info] Ready.")
	assert.Contains(t, view.Content, "assistant: Working")
	assert.Contains(t, view.Content, "Authorization: https://example.test/oauth")
	assert.Contains(t, view.Content, "Terminal: 100x40")
	assert.Contains(t, view.Content, "Request: hello|")
	assert.Contains(t, view.Content, "Ctrl+P next model | Shift+Ctrl+P previous model | Shift+Tab reasoning")
}

// TestModelRendersProvisionalToolCallNameFieldsAndPrefix preserves complete and prefix argument display.
func TestModelRendersProvisionalToolCallNameFieldsAndPrefix(t *testing.T) {
	t.Parallel()
	// Arrange a detached provisional call with complete and prefix fields.
	model := newRenderModel()
	model.snapshot.Body.ActiveToolCalls = map[string]presentation.ToolCallState{
		"call-1": {
			CallID: "call-1", Name: "read", Position: 1, Provisional: true,
			Fields: []presentation.ToolCallField{
				{Name: "path", Value: mo.Some[any]("file.txt"), Prefix: mo.None[string]()},
				{Name: "query", Value: mo.None[any](), Prefix: mo.Some("hel")},
			}, Arguments: nil,
		},
	}
	// Act by rendering the pending call.
	view := model.View().Content
	// Assert public argument display preserves field state without dumping a JSON object.
	assert.Contains(t, view, "[tool:call] read (provisional)")
	assert.Contains(t, view, `path="file.txt"`)
	assert.Contains(t, view, "query=hel")
	assert.NotContains(t, view, `{"path"`)
}
