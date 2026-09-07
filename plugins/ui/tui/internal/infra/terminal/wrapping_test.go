//go:build !integration

package terminal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelWrapsCompletedUnicodeContent preserves word, line, and terminal-cell boundaries.
func TestModelWrapsCompletedUnicodeContent(t *testing.T) {
	t.Parallel()
	// Arrange a completed Unicode block in a narrow viewport.
	model := newRenderModel()
	model.width = 16
	model.snapshot.Body.Transcript = []presentation.Line{
		renderTextLine(presentation.LineModel, "readable words wrap cleanly\n你好 世界"),
	}
	// Act by rendering the completed block.
	lines := model.visibleBodyLines(0)
	// Assert words wrap without corrupting Unicode or exceeding cell width.
	assert.Equal(t, []string{"assistant:", "readable words", "wrap cleanly", "你好 世界"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelWrapsActiveContent preserves wrapping of long streaming tokens.
func TestModelWrapsActiveContent(t *testing.T) {
	t.Parallel()
	// Arrange an active model block with a long token.
	model := newRenderModel()
	model.width = 16
	model.snapshot.Body.ActiveModel = map[int]presentation.ActiveModelContent{
		1: {Kind: mo.None[presentation.ModelContentKind](), Text: mo.Some("active words and supercalifragilistic")},
	}
	// Act by rendering the active block.
	lines := model.visibleBodyLines(0)
	// Assert long tokens split within the viewport.
	assert.Equal(t, []string{"assistant:", "active words and", "supercalifragili", "stic"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelClipsAfterWrapping uses wrapped visual lines for the height budget.
func TestModelClipsAfterWrapping(t *testing.T) {
	t.Parallel()
	// Arrange active content at two terminal sizes.
	model := newRenderModel()
	model.width, model.height = 16, fixedViewLineCount+2
	model.snapshot.Body.ActiveModel = map[int]presentation.ActiveModelContent{
		1: {Kind: mo.None[presentation.ModelContentKind](), Text: mo.Some("active words and supercalifragilistic")},
	}
	// Act by rendering with the bounded viewport and then without reported dimensions.
	bounded := model.visibleBodyLines(0)
	model.width, model.height = 0, 0
	unbounded := model.visibleBodyLines(0)
	// Assert clipping follows wrapping and retains the newest lines.
	assert.Equal(t, []string{"supercalifragili", "stic"}, bounded)
	assert.Equal(t, []string{"assistant: active words and supercalifragilistic"}, unbounded)
}

// TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight preserves the editor viewport.
func TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight(t *testing.T) {
	t.Parallel()
	// Arrange five completed blocks in a seven-line terminal.
	model := newRenderModel()
	model.width, model.height = 80, 7
	for _, text := range []string{"oldest", "older", "middle", "newer", "latest"} {
		model.snapshot.Body.Transcript = append(
			model.snapshot.Body.Transcript,
			renderTextLine(presentation.LineInformation, text),
		)
	}
	// Act by rendering the snapshot.
	view := model.View().Content
	// Assert output clipping never changes its input or hides the editor.
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), 7)
	assert.NotContains(t, view, "oldest")
	assert.Contains(t, view, "newer")
	assert.Contains(t, view, "latest")
	assert.Contains(t, view, "Status: Idle")
	assert.Contains(t, view, "Request: |")
	assert.Contains(t, view, "Ctrl+Q quit")
	assert.Len(t, model.snapshot.Body.Transcript, 5)
}

// TestRenderLineDistinguishesRefusal preserves the refusal display category.
func TestRenderLineDistinguishesRefusal(t *testing.T) {
	t.Parallel()
	// Arrange a refusal block distinct from ordinary model text.
	line := renderTextLine(presentation.LineRefusal, "cannot help")
	// Act by rendering the typed block.
	text := renderLine(line)
	// Assert the renderer retains the refusal prefix.
	assert.Equal(t, "[refusal] cannot help", text)
}

// TestModelRendersReasoningExpansion preserves one marker per block and full expanded content.
func TestModelRendersReasoningExpansion(t *testing.T) {
	t.Parallel()
	// Arrange completed and active reasoning blocks with distinct text.
	model := newRenderModel()
	model.snapshot.Body.Transcript = []presentation.Line{
		renderTextLine(presentation.LineReasoning, "completed reasoning"),
	}
	model.snapshot.Body.ActiveModel = map[int]presentation.ActiveModelContent{
		0: {Kind: mo.Some(presentation.ModelContentReasoning), Text: mo.Some("active reasoning")},
	}
	// Act by rendering the two application-selected expansion states.
	collapsed := strings.Join(model.visibleBodyLines(0), "\n")
	model.snapshot.ReasoningExpanded = true
	expanded := strings.Join(model.visibleBodyLines(0), "\n")
	// Assert collapsed output omits content while expanded output preserves both blocks.
	assert.NotContains(t, collapsed, "completed reasoning")
	assert.NotContains(t, collapsed, "active reasoning")
	assert.Contains(t, expanded, "completed reasoning")
	assert.Contains(t, expanded, "active reasoning")
}
