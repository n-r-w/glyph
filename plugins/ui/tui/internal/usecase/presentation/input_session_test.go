//go:build !integration

package presentation

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestModelClearsSessionCommandOnlyAfterHostConfirmsReplacement verifies rejected replacement keeps the draft until
// confirmation.
func TestModelClearsSessionCommandOnlyAfterHostConfirmsReplacement(t *testing.T) {
	t.Parallel()

	// Arrange a pending session command and delayed host confirmation.
	commands := make([]Command, 0, 1)
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})
	model.model.input = []rune("/new")
	model.model.cursor = len(model.model.input)

	// Act by submitting the command and then applying confirmed session replacement.
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	require.Len(t, commands, 1)
	assert.Equal(t, CommandCreateSession, commands[0].Kind)
	assert.Equal(t, "/new", string(model.model.input))

	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 eventSessionChanged,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo: mo.Some(SessionInfo{
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
	assert.Empty(t, model.model.input)
	assert.Zero(t, model.model.cursor)

	model.model.input = []rune("/session")
	model.model.cursor = len(model.model.input)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	assert.Equal(t, "/session", string(model.model.input))
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 eventSessionInformation,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo: mo.Some(SessionInfo{
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
	assert.Empty(t, model.model.input)
	assert.Zero(t, model.model.cursor)
	view := model.model.state.Transcript[len(model.model.state.Transcript)-1].Text.OrEmpty()
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
	commands := make([]Command, 0, 2)
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})
	model.model.input = []rune("/resume")
	model.model.cursor = len(model.model.input)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	require.Len(t, commands, 1)
	assert.Equal(t, CommandListSessions, commands[0].Kind)
	model.model.resumeStatus = "stale rejection"

	model = updateModel(t, model, event{
		RestoredTranscript:   nil,
		Kind:                 eventSessionList,
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions: []SessionSummary{
			{
				Info: SessionInfo{
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
				Info: SessionInfo{
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
				Info: SessionInfo{
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
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
	})
	assert.True(t, model.model.selectorOpen)
	assert.True(t, model.model.sessionSelector)
	assert.Empty(t, model.model.resumeStatus)
	assert.True(t, model.model.sessionSelector)
	assert.Equal(t, "id-fallback", model.model.state.Sessions[2].Info.ID)
	assert.Equal(t, "/resume", string(model.model.input))

	model.model.state.SessionInfo = mo.Some(SessionInfo{
		ID: "active", Name: "active", NamePresent: true, WorkingDirectory: "/project",
		StoragePath: "/active", StoragePresent: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(5, 0),
	})
	model.model.state.Transcript = []Line{{
		Kind: LineInformation, ToolName: mo.None[string](), Status: mo.None[string](),
		Text: mo.Some("existing transcript"), Contents: mo.None[[]Content](),
	}}
	model.model.input = []rune("preserved draft")
	model.model.cursor = len(model.model.input)
	// Act by selecting the second session and confirming resume.
	model = updateModel(t, model, testKey(inputcontroller.KeyDown))
	beforeSessions := append([]SessionSummary(nil), model.model.state.Sessions...)
	beforeTranscript := append([]Line(nil), model.model.state.Transcript...)
	beforeInfo := model.model.state.SessionInfo
	beforeInput := append([]rune(nil), model.model.input...)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	// Assert the selected session command is emitted without mutating the selector data or draft.
	require.Len(t, commands, 2)
	assert.Equal(t, CommandResumeSession, commands[1].Kind)
	assert.Equal(t, "second", commands[1].SessionID.MustGet())
	assert.True(t, model.model.selectorOpen)
	assert.True(t, model.model.sessionSelector)
	assert.True(t, model.model.resumePending)
	assert.Equal(t, 1, model.model.selectorRow)
	model = updateModel(t, model, testKey(inputcontroller.KeyEnter))
	assert.Len(t, commands, 2)

	model = updateModel(t, model, event{
		RestoredTranscript:   nil,
		Kind:                 eventInformation,
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.Some("session persistence failed"),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	assert.True(t, model.model.selectorOpen)
	assert.True(t, model.model.sessionSelector)
	assert.False(t, model.model.resumePending)
	assert.Equal(t, 1, model.model.selectorRow)
	assert.Equal(t, beforeSessions, model.model.state.Sessions)
	assert.Equal(t, beforeTranscript, model.model.state.Transcript)
	assert.Equal(t, beforeInfo, model.model.state.SessionInfo)
	assert.Equal(t, beforeInput, model.model.input)
	assert.Contains(t, model.model.resumeStatus, "session persistence failed")

	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	require.Len(t, commands, 3)
	assert.Equal(t, CommandResumeSession, commands[2].Kind)
	assert.True(t, model.model.resumePending)
	assert.NotContains(t, model.model.resumeStatus, "session persistence failed")

	restored := []Line{
		{
			Kind: LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some("prior-user"), Contents: mo.None[[]Content](),
		},
		{
			Kind: LineModel, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some("prior-model"), Contents: mo.None[[]Content](),
		},
	}
	model = updateModel(t, model, event{
		RestoredTranscript:   restored,
		Kind:                 eventSessionChanged,
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.Some(model.model.state.Sessions[1].Info),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	assert.False(t, model.model.selectorOpen)
	assert.False(t, model.model.sessionSelector)
	assert.Equal(t, restored, model.model.state.Transcript)
	assert.Empty(t, model.model.resumeStatus)
	assert.Empty(t, model.model.input)
	assert.Zero(t, model.model.cursor)
}

// TestModelEscapeClearsResumeRejection verifies Escape closes the selector and clears rejection and draft state.
func TestModelEscapeClearsResumeRejection(t *testing.T) {
	t.Parallel()

	// Arrange an open resume selector with rejection text, a resume draft, and one stored session.
	model := newTestModel(t, AvailabilityIdle, func(Command) error { return nil })
	model.model.selectorOpen = true
	model.model.sessionSelector = true
	model.model.resumeStatus = "Session replacement is unavailable."
	model.model.input = []rune("/resume")
	model.model.cursor = len(model.model.input)
	model.model.state.Sessions = []SessionSummary{{
		Info: SessionInfo{
			ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
			StoragePath: "/stored", StoragePresent: true,
			CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
		},
		FirstUserText: "", TextPresent: false, TotalMessages: 0,
	}}

	// Act by sending Escape through the model update path.
	model = updateModel(t, model, testKey(inputcontroller.KeyEscape))

	// Assert selector state, rejection text, and draft input are cleared from state and view.
	assert.False(t, model.model.selectorOpen)
	assert.False(t, model.model.sessionSelector)
	assert.Empty(t, model.model.resumeStatus)
	assert.Empty(t, model.model.input)
	assert.Zero(t, model.model.cursor)
	assert.NotContains(t, model.model.resumeStatus, "Session replacement is unavailable.")
}

// TestFormatSessionInfoShowsAbsentOptionalFields verifies absent name and storage path use explicit placeholders.
func TestFormatSessionInfoShowsAbsentOptionalFields(t *testing.T) {
	t.Parallel()

	// Arrange session information whose optional name and storage path are absent.

	// Act by formatting that session information for display.
	text := formatSessionInfo(SessionInfo{
		ID: "startup", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	})

	// Assert both absent optional fields use the explicit placeholder.
	assert.Contains(t, text, "Name: <absent>")
	assert.Contains(t, text, "Storage path: <absent>")
}
