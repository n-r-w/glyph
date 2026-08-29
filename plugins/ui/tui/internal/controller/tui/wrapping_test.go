package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestModelReasoningUsesOneLocalCollapsedToggle verifies ordered markers, one shared toggle, and wrapped expansion.
func TestModelReasoningUsesOneLocalCollapsedToggle(t *testing.T) {
	t.Parallel()

	// Arrange reasoning transcript lines in a narrow terminal.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model.state.Transcript = append(model.state.Transcript,
		presentationdomain.Line{
			Kind:     presentationdomain.LineReasoning,
			Text:     mo.Some("first reasoning block"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		presentationdomain.Line{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("between blocks"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		presentationdomain.Line{
			Kind:     presentationdomain.LineReasoning,
			Text:     mo.Some("second reasoning block"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	)
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  12,
		Height: 0,
	})
	view := model.View().Content
	assert.Contains(t, view, "Ctrl+T reasoning display")
	assert.NotContains(t, view, "Ctrl+O reasoning display")

	collapsed := strings.Join(model.visibleBodyLines(0), "\n")
	assert.Equal(t, 2, strings.Count(collapsed, "[collapsed]"))
	assert.NotContains(t, collapsed, "first reasoning")
	firstMarker := strings.Index(collapsed, "[collapsed]")
	between := strings.Index(collapsed, "between")
	secondMarker := strings.LastIndex(collapsed, "[collapsed]")
	assert.Less(t, firstMarker, between)
	assert.Less(t, between, secondMarker)
	assert.Len(t, model.state.Transcript, 3)

	model.emitting = true
	model.selectorOpen = true
	// Act by toggling reasoning expansion and rendering the expanded lines.
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'o',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.False(t, model.reasoningExpanded)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        't',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.True(t, model.reasoningExpanded)
	assert.True(t, model.emitting)
	assert.True(t, model.selectorOpen)
	expandedLines := model.visibleBodyLines(0)
	expanded := strings.Join(expandedLines, "\n")
	assert.Contains(t, expanded, "first")
	assert.Contains(t, expanded, "second")
	// Assert expanded reasoning remains within terminal width.
	for _, line := range expandedLines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 12)
	}
}

// TestModelWrapsCompletedUnicodeContent verifies readable wrapping, display width, and embedded line boundaries.
func TestModelWrapsCompletedUnicodeContent(t *testing.T) {
	t.Parallel()

	// Arrange completed mixed Unicode content in a narrow terminal.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("readable words wrap cleanly\n你好 世界"),
		}},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		Position:          mo.None[int](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: 0,
	})

	// Act by computing visible wrapped body lines.
	lines := model.visibleBodyLines(0)
	// Assert completed content wraps at cell boundaries without corrupting Unicode.
	assert.Equal(t, []string{"assistant:", "readable words", "wrap cleanly", "你好 世界"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelWrapsActiveContent verifies word wrapping and long-token splitting for active streaming text.
func TestModelWrapsActiveContent(t *testing.T) {
	t.Parallel()

	// Arrange active model content with a long unbroken word.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("active words and supercalifragilistic"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: 0,
	})

	// Act by computing visible wrapped body lines.
	lines := model.visibleBodyLines(0)
	// Assert active content wraps words and clips long tokens to terminal width.
	assert.Equal(t, []string{"assistant:", "active words and", "supercalifragili", "stic"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelClipsAfterWrapping verifies that the height budget selects wrapped visual lines.
func TestModelClipsAfterWrapping(t *testing.T) {
	t.Parallel()

	// Arrange wrapped active content and two terminal heights.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("active words and supercalifragilistic"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	// Act by resizing after content wrapping.
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: fixedViewLineCount + 2,
	})
	// Assert clipping retains only lines that fit each terminal height.
	assert.Equal(t, []string{"supercalifragili", "stic"}, model.visibleBodyLines(0))

	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  0,
		Height: 0,
	})
	assert.Equal(t, []string{"assistant: active words and supercalifragilistic"}, model.visibleBodyLines(0))
}

// TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight verifies viewport truncation.
func TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight(t *testing.T) {
	t.Parallel()

	// Arrange five transcript lines, an editor draft, and a short terminal.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	for _, text := range []string{"oldest", "older", "middle", "newer", "latest"} {
		model = updateModel(t, model, presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventInformation,
			Text:                 mo.Some(text),
			Startup:              nil,
			Extensions:           nil,
			Availability:         mo.None[presentationdomain.Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		})
	}
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  80,
		Height: 7,
	})

	// Act by rendering the constrained terminal view.
	view := model.View().Content
	// Assert the editor and latest transcript remain visible while older lines are clipped.
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), 7)
	assert.NotContains(t, view, "oldest")
	assert.Contains(t, view, "newer")
	assert.Contains(t, view, "latest")
	assert.Contains(t, view, "Status: Idle")
	assert.Contains(t, view, "Request: |")
	assert.Contains(t, view, "Ctrl+Q quit")
	assert.Len(t, model.state.Transcript, 5)
}

// TestModelRetainsTranscriptWhenReturningToIdleForSecondTurn verifies editor reuse after settlement.
func TestModelRetainsTranscriptWhenReturningToIdleForSecondTurn(t *testing.T) {
	t.Parallel()

	// Arrange a completed first response followed by an idle second draft.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	// Act by settling the first turn and entering the second request.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("first response"),
		}},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		Position:          mo.None[int](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAgentSettled,
		Text:                 mo.Some("completed"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
		Startup:              nil,
		Extensions:           nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "second request",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	// Assert the first transcript and second draft remain visible together.
	assert.Contains(t, model.View().Content, "assistant: first response")
	assert.Contains(t, model.View().Content, "Request: second request|")
}

// TestRenderLineDistinguishesRefusal verifies refusal text has its own terminal prefix.
func TestRenderLineDistinguishesRefusal(t *testing.T) {
	t.Parallel()

	// Arrange a refusal transcript line.
	line := presentationdomain.Line{
		Kind:     presentationdomain.LineRefusal,
		Text:     mo.Some("cannot help"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}

	// Act by rendering the refusal line.
	result := renderLine(line)

	// Assert the rendered prefix distinguishes refusal from model text.
	assert.Equal(t, "[refusal] cannot help", result)
}
