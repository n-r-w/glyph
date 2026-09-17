//go:build !integration

package terminal

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestWrappedBodyLinksKeepCompleteTargets checks targets independently of wrapping and clipping.
func TestWrappedBodyLinksKeepCompleteTargets(t *testing.T) {
	t.Parallel()
	// Arrange an OAuth-shaped URL with punctuation that belongs to its query.
	const target = "https://example.test/oauth?redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fcallback" +
		"&state=a_b-c&scope=openid+profile"
	for _, width := range []int{0, 16, 80, 218} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()
			// Act by wrapping before the viewport can discard earlier rows.
			lines := wrappedBodyLines(target, width)
			// Assert every standalone row links all visible characters to the entire address.
			var visible strings.Builder
			for _, line := range lines {
				text, targets := hyperlinkCells(t, line)
				visible.WriteString(text)
				require.NotEmpty(t, targets)
				for _, destination := range targets {
					assert.Equal(t, target, destination)
				}
				if width > 0 {
					assert.LessOrEqual(t, ansi.StringWidth(line), width)
				}
			}
			assert.Equal(t, target, visible.String())
		})
	}
}

// TestBodyLinkRecognition checks ordinary URLs, inline Markdown, and surrounding punctuation.
func TestBodyLinkRecognition(t *testing.T) {
	t.Parallel()
	// Arrange complete targets and the visible characters that should remain clickable.
	tests := []struct {
		name    string
		input   string
		visible string
		linked  string
		target  string
	}{
		{
			name:    "bare URL",
			input:   "See https://example.test/a?x=1&y=2#part now",
			visible: "See https://example.test/a?x=1&y=2#part now",
			linked:  "https://example.test/a?x=1&y=2#part",
			target:  "https://example.test/a?x=1&y=2#part",
		},
		{
			name:    "sentence punctuation",
			input:   "See (https://example.test/a).",
			visible: "See (https://example.test/a).",
			linked:  "https://example.test/a",
			target:  "https://example.test/a",
		},
		{
			name:    "balanced path",
			input:   "https://example.test/Function_(math).",
			visible: "https://example.test/Function_(math).",
			linked:  "https://example.test/Function_(math)",
			target:  "https://example.test/Function_(math)",
		},
		{
			name:    "Markdown label",
			input:   "Read [the guide](https://example.test/guide) now",
			visible: "Read the guide now",
			linked:  "the guide",
			target:  "https://example.test/guide",
		},
		{
			name:    "Markdown nested parentheses",
			input:   "[guide](https://example.test/Function_(math))",
			visible: "guide",
			linked:  "guide",
			target:  "https://example.test/Function_(math)",
		},
		{
			name:    "prefixed Markdown",
			input:   "[info] Read [guide](https://example.test/guide)",
			visible: "[info] Read guide",
			linked:  "guide",
			target:  "https://example.test/guide",
		},
		{
			name:    "explicit target punctuation",
			input:   "[query](https://example.test/?q=!)",
			visible: "query",
			linked:  "query",
			target:  "https://example.test/?q=!",
		},
		{
			name:    "Unicode label",
			input:   "[Документы](https://example.test/docs)",
			visible: "Документы",
			linked:  "Документы",
			target:  "https://example.test/docs",
		},
		{
			name:    "angle autolink",
			input:   "<http://localhost:8080/page>",
			visible: "<http://localhost:8080/page>",
			linked:  "http://localhost:8080/page",
			target:  "http://localhost:8080/page",
		},
		{
			name:    "quoted tool output",
			input:   `url="https://example.test/path"`,
			visible: `url="https://example.test/path"`,
			linked:  "https://example.test/path",
			target:  "https://example.test/path",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Act through the common body renderer with no width constraint.
			lines := wrappedBodyLines(test.input, 0)
			// Assert only the intended text carries the full target.
			require.Len(t, lines, 1)
			visible, targets := hyperlinkCells(t, lines[0])
			assert.Equal(t, test.visible, visible)
			var linked strings.Builder
			for index, character := range []rune(visible) {
				if targets[index] != "" {
					assert.Equal(t, test.target, targets[index])
					linked.WriteRune(character)
				}
			}
			assert.Equal(t, test.linked, linked.String())
		})
	}
}

// TestBodyLinksDoNotLeakIntoAdjacentText checks two independent targets and an existing OSC link.
func TestBodyLinksDoNotLeakIntoAdjacentText(t *testing.T) {
	t.Parallel()
	// Arrange both generated and existing hyperlinks with ordinary text between them.
	const first = "https://one.test/path"
	const second = "http://two.test/path"
	input := first + " gap " + ansi.SetHyperlink(second) + "existing" + ansi.ResetHyperlink() + " tail"
	// Act by rendering and wrapping both links.
	lines := wrappedBodyLines(input, 12)
	// Assert links stay closed per row, existing targets are not nested, and gaps are not linked.
	var linked strings.Builder
	for _, line := range lines {
		visible, targets := hyperlinkCells(t, line)
		for index, character := range []rune(visible) {
			switch targets[index] {
			case first, second:
				linked.WriteRune(character)
			default:
				assert.Empty(t, targets[index])
			}
		}
	}
	assert.Equal(t, first+"existing", linked.String())
}

// TestModelHyperlinksCoverBodySources checks common rendering for published body data.
func TestModelHyperlinksCoverBodySources(t *testing.T) {
	t.Parallel()
	// Arrange the same long URL in each independently published display source.
	const target = "https://example.test/long/path?first=one&second=two"
	sources := map[string]func(*Model){
		"authorization": func(model *Model) { model.snapshot.Body.AuthorizationURL = mo.Some(target) },
		"startup": func(model *Model) {
			model.snapshot.Body.Startup = []presentation.Line{renderTextLine(presentation.LineInformation, target)}
		},
		"completed model": func(model *Model) {
			model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineModel, target)}
		},
		"active model": func(model *Model) {
			model.snapshot.Body.ActiveModel = map[int]presentation.ActiveModelContent{
				0: {Kind: mo.None[presentation.ModelContentKind](), Text: mo.Some(target)},
			}
		},
		"tool output": func(model *Model) {
			model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineToolStdout, target)}
		},
		"tool error": func(model *Model) {
			model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineToolStderr, target)}
		},
		"user": func(model *Model) {
			model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineUser, target)}
		},
		"reasoning": func(model *Model) {
			model.snapshot.ReasoningExpanded = true
			model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineReasoning, target)}
		},
		"branch summary": func(model *Model) {
			model.snapshot.BranchSummariesExpanded = true
			model.snapshot.Body.Transcript = []presentation.Line{renderTextLine(presentation.LineBranchSummary, target)}
		},
	}
	for name, arrange := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model := newRenderModel()
			model.width, model.height = 16, fixedViewLineCount+1
			arrange(&model)
			// Act after wrapping discards the beginning of the long URL.
			lines := model.visibleBodyLines(0)
			// Assert the final visible fragment alone still carries the full target.
			require.Len(t, lines, 1)
			_, targets := hyperlinkCells(t, lines[0])
			require.NotEmpty(t, targets)
			for _, destination := range targets {
				assert.Equal(t, target, destination)
			}
		})
	}
}

// TestEllipsizedURLRetainsTarget checks that selector truncation cannot shorten the click target.
func TestEllipsizedURLRetainsTarget(t *testing.T) {
	t.Parallel()
	// Arrange a URL longer than a selector row.
	const target = "https://example.test/long/path"
	// Act through the shared selector clipping helper.
	line := ellipsize(target, 16)
	// Assert visible text fits while clickable text still addresses the complete URL.
	visible, targets := hyperlinkCells(t, line)
	assert.Equal(t, "https://example…", visible)
	assert.Contains(t, targets, target)
	assert.LessOrEqual(t, ansi.StringWidth(line), 16)
}

// TestTreeAndStatusLinksKeepTargets covers display paths outside transcript wrapping.
func TestTreeAndStatusLinksKeepTargets(t *testing.T) {
	t.Parallel()
	// Arrange a long target in a tree preview and the published status text.
	const target = "https://example.test/long/path?token=complete"
	model := newRenderModel()
	model.width = 32
	model.snapshot.TreeStatus = target
	row := treeRows([]presentation.VisibleEntry{visibleEntry("entry", mo.None[string](), target)})[0]
	// Act through both read-only render paths.
	tree := model.renderTreeRow(row, true)
	view := model.View().Content
	// Assert a truncated preview and the status retain the original click target.
	_, treeTargets := hyperlinkCells(t, tree)
	assert.Contains(t, treeTargets, target)
	require.Contains(t, ansi.Strip(view), target)
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(ansi.Strip(line), target) {
			_, statusTargets := hyperlinkCells(t, line)
			assert.Contains(t, statusTargets, target)
		}
	}
}

// TestLinkRenderingPreservesEditableText keeps Markdown input and cursor positions literal.
func TestLinkRenderingPreservesEditableText(t *testing.T) {
	t.Parallel()
	// Arrange the same editable Markdown in the request editor and tree label editor.
	const input = "[label](https://example.test/path)"
	model := newRenderModel()
	model.snapshot.Input, model.snapshot.Cursor = []rune(input), len(input)
	model.snapshot.TreeMode = presentation.TreeLabel
	model.snapshot.TreePanel = mo.Some(presentation.TreeView{})
	model.snapshot.TreeInput, model.snapshot.TreeCursor = []rune(input), len(input)
	// Act by rendering both editors alongside read-only UI content.
	view := model.View().Content
	// Assert both editors show the original text and the cursor after the closing parenthesis.
	assert.Equal(t, 2, strings.Count(view, input+"|"))
}

// TestLinkRenderingPreservesIncompleteText keeps streaming fragments and ordinary output readable.
func TestLinkRenderingPreservesIncompleteText(t *testing.T) {
	t.Parallel()
	// Arrange text that does not yet contain a complete web destination.
	for _, input := range []string{"plain output", "[guide](https://", "http://%zz", "mailto:user@example.test"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			// Act through the same renderer used for each streaming update.
			lines := wrappedBodyLines(input, 0)
			// Assert incomplete input remains readable and unchanged.
			assert.Equal(t, []string{input}, lines)
		})
	}
}

// TestExistingMultilineLinkRetainsParameters keeps tool-supplied terminal hyperlinks self-contained per row.
func TestExistingMultilineLinkRetainsParameters(t *testing.T) {
	t.Parallel()
	// Arrange an ST-terminated link spanning explicit newlines, as emitted by terminal-aware tools.
	const target = "https://example.test/tool"
	const opening = "\x1b]8;id=tool;" + target + "\x1b\\"
	input := opening + "first\nsecond" + "\x1b]8;;\x1b\\"
	// Act by rendering the already linked text.
	lines := wrappedBodyLines(input, 20)
	// Assert both rows retain the parameterized target and independently close their link.
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.True(t, strings.HasPrefix(line, opening))
		_, targets := hyperlinkCells(t, line)
		for _, destination := range targets {
			assert.Equal(t, target, destination)
		}
	}
}

// hyperlinkCells interprets OSC 8 metadata and checks that each rendered row closes its links.
func hyperlinkCells(t *testing.T, line string) (string, []string) {
	t.Helper()
	var visible strings.Builder
	var targets []string
	target := ""
	state := ansi.NormalState
	for len(line) > 0 {
		sequence, width, size, next := ansi.DecodeSequence(line, state, nil)
		state = next
		line = line[size:]
		if strings.HasPrefix(sequence, "\x1b]8;") {
			body := strings.TrimSuffix(strings.TrimSuffix(sequence, "\x07"), "\x1b\\")
			_, destination, found := strings.Cut(strings.TrimPrefix(body, "\x1b]8;"), ";")
			require.True(t, found)
			if destination != "" {
				assert.Empty(t, target, "hyperlinks must not nest")
			}
			target = destination
		} else if width > 0 {
			visible.WriteString(sequence)
			for range []rune(sequence) {
				targets = append(targets, target)
			}
		}
	}
	assert.Empty(t, target, "a visible row must not leak a hyperlink into the next row")
	return visible.String(), targets
}
