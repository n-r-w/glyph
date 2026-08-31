//go:build !integration

package tui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestModelClearsSessionCommandOnlyAfterHostConfirmsReplacement verifies rejected replacement keeps the draft until
// confirmation.
func TestModelClearsSessionCommandOnlyAfterHostConfirmsReplacement(t *testing.T) {
	t.Parallel()

	// Arrange a pending session command and delayed host confirmation.
	commands := make([]presentationdomain.Command, 0, 1)
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model.input = []rune("/new")
	model.cursor = len(model.input)

	// Act by submitting the command and then applying confirmed session replacement.
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 1)
	assert.Equal(t, presentationdomain.CommandCreateSession, commands[0].Kind)
	assert.Equal(t, "/new", string(model.input))

	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventSessionChanged,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.Some(presentationdomain.SessionInfo{
			ID:               "new-session",
			Name:             "",
			NamePresent:      false,
			WorkingDirectory: "/project",
			StoragePath:      "",
			StoragePresent:   false,
			CreatedAt:        time.Unix(1, 0),
			UpdatedAt:        time.Unix(1, 0),
		}),
	}))
	// Assert the draft clears only after host confirmation.
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)

	model.input = []rune("/session")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	assert.Equal(t, "/session", string(model.input))
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventSessionInformation,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.Some(presentationdomain.SessionInfo{
			ID:               "new-session",
			Name:             "renamed",
			NamePresent:      true,
			WorkingDirectory: "/project",
			StoragePath:      "/sessions/new-session.jsonl",
			StoragePresent:   true,
			CreatedAt:        time.Unix(1, 0),
			UpdatedAt:        time.Unix(2, 0),
		}),
	}))
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
	view := model.View().Content
	assert.Contains(t, view, "Session ID: new-session")
	assert.Contains(t, view, "Name: renamed")
	assert.Contains(t, view, "Working directory: /project")
	assert.Contains(t, view, "Storage path: /sessions/new-session.jsonl")
	assert.Contains(t, view, "Created: 1970-01-01T00:00:01Z")
	assert.Contains(t, view, "Updated: 1970-01-01T00:00:02Z")
}

// TestModelResumeSelectorEmitsSelectedSession verifies selector navigation emits the chosen ID without mutating the
// draft.
func TestModelResumeSelectorEmitsSelectedSession(t *testing.T) {
	t.Parallel()

	// Arrange a resume selector with two sessions and a preserved draft.
	commands := make([]presentationdomain.Command, 0, 2)
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model.input = []rune("/resume")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 1)
	assert.Equal(t, presentationdomain.CommandListSessions, commands[0].Kind)
	model.resumeStatus = "stale rejection"

	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventSessionList,
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
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions: []presentationdomain.SessionSummary{
			{
				Info: presentationdomain.SessionInfo{
					ID:               "first",
					Name:             "first",
					NamePresent:      true,
					WorkingDirectory: "/project",
					StoragePath:      "/first",
					StoragePresent:   true,
					CreatedAt:        time.Unix(1, 0),
					UpdatedAt:        time.Unix(2, 0),
				},
				FirstUserText: "",
				TextPresent:   false,
				TotalMessages: 1,
			},
			{
				Info: presentationdomain.SessionInfo{
					ID:               "second",
					Name:             "",
					NamePresent:      false,
					WorkingDirectory: "/project",
					StoragePath:      "/second",
					StoragePresent:   true,
					CreatedAt:        time.Unix(1, 0),
					UpdatedAt:        time.Unix(3, 0),
				},
				FirstUserText: "fallback",
				TextPresent:   true,
				TotalMessages: 2,
			},
			{
				Info: presentationdomain.SessionInfo{
					ID:               "id-fallback",
					Name:             "",
					NamePresent:      false,
					WorkingDirectory: "/project",
					StoragePath:      "/third",
					StoragePresent:   true,
					CreatedAt:        time.Unix(1, 0),
					UpdatedAt:        time.Unix(4, 0),
				},
				FirstUserText: "",
				TextPresent:   false,
				TotalMessages: 0,
			},
		},
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:         mo.None[presentationdomain.TreeEvent](),
	})
	assert.True(t, model.selectorOpen)
	assert.True(t, model.sessionSelector)
	assert.Empty(t, model.resumeStatus)
	model.width = 100
	assert.Contains(t, model.View().Content, "Sessions:")
	assert.Contains(t, model.View().Content, "id-fallback")
	assert.Equal(t, "/resume", string(model.input))

	model.state.SessionInfo = mo.Some(presentationdomain.SessionInfo{
		ID: "active", Name: "active", NamePresent: true, WorkingDirectory: "/project",
		StoragePath: "/active", StoragePresent: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(5, 0),
	})
	model.state.Transcript = []presentationdomain.Line{{
		Kind: presentationdomain.LineInformation, ToolName: mo.None[string](), Status: mo.None[string](),
		Text: mo.Some("existing transcript"), Contents: mo.None[[]presentationdomain.Content](),
	}}
	model.input = []rune("preserved draft")
	model.cursor = len(model.input)
	// Act by selecting the second session and confirming resume.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyDown)))
	beforeSessions := append([]presentationdomain.SessionSummary(nil), model.state.Sessions...)
	beforeTranscript := append([]presentationdomain.Line(nil), model.state.Transcript...)
	beforeInfo := model.state.SessionInfo
	beforeInput := append([]rune(nil), model.input...)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	// Assert the selected session command is emitted without mutating the selector data or draft.
	require.Len(t, commands, 2)
	assert.Equal(t, presentationdomain.CommandResumeSession, commands[1].Kind)
	assert.Equal(t, "second", commands[1].SessionID.MustGet())
	assert.True(t, model.selectorOpen)
	assert.True(t, model.sessionSelector)
	assert.True(t, model.resumePending)
	assert.Equal(t, 1, model.selectorRow)
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	assert.Len(t, commands, 2)

	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInformation, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.Some("session persistence failed"),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:         mo.None[presentationdomain.TreeEvent](),
	})
	assert.True(t, model.selectorOpen)
	assert.True(t, model.sessionSelector)
	assert.False(t, model.resumePending)
	assert.Equal(t, 1, model.selectorRow)
	assert.Equal(t, beforeSessions, model.state.Sessions)
	assert.Equal(t, beforeTranscript, model.state.Transcript)
	assert.Equal(t, beforeInfo, model.state.SessionInfo)
	assert.Equal(t, beforeInput, model.input)
	assert.Contains(t, model.View().Content, "session persistence failed")

	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 3)
	assert.Equal(t, presentationdomain.CommandResumeSession, commands[2].Kind)
	assert.True(t, model.resumePending)
	assert.NotContains(t, model.View().Content, "session persistence failed")

	restored := []presentationdomain.Line{
		{
			Kind: presentationdomain.LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some("prior-user"), Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind: presentationdomain.LineModel, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some("prior-model"), Contents: mo.None[[]presentationdomain.Content](),
		},
	}
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: restored,
		Kind:               presentationdomain.EventSessionChanged, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.Some(model.state.Sessions[1].Info), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:         mo.None[presentationdomain.TreeEvent](),
	})
	assert.False(t, model.selectorOpen)
	assert.False(t, model.sessionSelector)
	assert.Equal(t, restored, model.state.Transcript)
	assert.Empty(t, model.resumeStatus)
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
}

// TestModelResumeRejectionReservesHeightAndFitsTerminalWidth verifies status layout consumes height without overflow.
func TestModelResumeRejectionReservesHeightAndFitsTerminalWidth(t *testing.T) {
	t.Parallel()

	// Arrange an open resume selector with rejection text, constrained dimensions, and stored-session rows.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error { return nil })
	model.selectorOpen = true
	model.sessionSelector = true
	model.resumeStatus = "Session replacement is unavailable because another operation is active."
	model.width = 24
	model.height = fixedViewLineCount + selectorFixedLineCount + 1 + 2
	for index := range 5 {
		model.state.Sessions = append(model.state.Sessions, presentationdomain.SessionSummary{
			Info: presentationdomain.SessionInfo{
				ID: fmt.Sprintf("stored-%d", index), Name: "", NamePresent: false, WorkingDirectory: "/project",
				StoragePath: "/stored", StoragePresent: true,
				CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(int64(index+2), 0),
			},
			FirstUserText: "", TextPresent: false, TotalMessages: 0,
		})
	}

	// Act by calculating the visible selector lines.
	lines := model.visibleSelectorLines()

	// Assert the status reserves one line, remains visible, and fits the terminal cell width.
	require.Len(t, lines, selectorFixedLineCount+1+2)
	assert.Contains(t, lines[len(lines)-2], "Session status:")
	assert.LessOrEqual(t, ansi.StringWidth(lines[len(lines)-2]), model.width)
}

// TestModelEscapeClearsResumeRejection verifies Escape closes the selector and clears rejection and draft state.
func TestModelEscapeClearsResumeRejection(t *testing.T) {
	t.Parallel()

	// Arrange an open resume selector with rejection text, a resume draft, and one stored session.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error { return nil })
	model.selectorOpen = true
	model.sessionSelector = true
	model.resumeStatus = "Session replacement is unavailable."
	model.input = []rune("/resume")
	model.cursor = len(model.input)
	model.state.Sessions = []presentationdomain.SessionSummary{{
		Info: presentationdomain.SessionInfo{
			ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
			StoragePath: "/stored", StoragePresent: true,
			CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
		},
		FirstUserText: "", TextPresent: false, TotalMessages: 0,
	}}

	// Act by sending Escape through the model update path.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEscape)))

	// Assert selector state, rejection text, and draft input are cleared from state and view.
	assert.False(t, model.selectorOpen)
	assert.False(t, model.sessionSelector)
	assert.Empty(t, model.resumeStatus)
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
	assert.NotContains(t, model.View().Content, "Session replacement is unavailable.")
}

// TestFormatSessionInfoShowsAbsentOptionalFields verifies absent name and storage path use explicit placeholders.
func TestFormatSessionInfoShowsAbsentOptionalFields(t *testing.T) {
	t.Parallel()

	// Arrange session information whose optional name and storage path are absent.

	// Act by formatting that session information for display.
	text := formatSessionInfo(presentationdomain.SessionInfo{
		ID: "startup", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	})

	// Assert both absent optional fields use the explicit placeholder.
	assert.Contains(t, text, "Name: <absent>")
	assert.Contains(t, text, "Storage path: <absent>")
}
