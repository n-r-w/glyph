package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestServiceAppliesInitializationAndLifecycleWithoutOwningHostState verifies ordered projection only.
func TestServiceAppliesInitializationAndLifecycleWithoutOwningHostState(t *testing.T) {
	t.Parallel()

	// Arrange a presentation service and a complete initialization state.
	service := New()
	state := service.Apply(presentationdomain.State{}, testInitializationEvent(
		[]presentationdomain.Line{
			{
				Kind:     presentationdomain.LineInformation,
				Text:     mo.Some("Glyph session initialized."),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			},
			{
				Kind:     presentationdomain.LineError,
				Text:     mo.Some("Optional extension is unavailable."),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			},
		},
		presentationdomain.AvailabilityIdle,
		[]presentationdomain.Extension{
			{
				ID:    "tools",
				Tools: []string{"read", "edit"},
				Path:  "",
			},
		},
	))

	require.Len(t, state.Startup, 2)
	assert.Equal(t, presentationdomain.Line{
		Kind:     presentationdomain.LineInformation,
		Text:     mo.Some("Glyph session initialized."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}, state.Startup[0])
	assert.Equal(t, presentationdomain.LineError, state.Startup[1].Kind)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)

	// Act by applying model, tool, error, authorization, and settlement lifecycle events.
	state = service.Apply(state, testModelDeltaEvent(1, presentationdomain.ModelContentText, "Hel"))
	state = service.Apply(state, testModelDeltaEvent(1, presentationdomain.ModelContentText, "lo"))
	state = service.Apply(state, testModelDeltaEvent(0, presentationdomain.ModelContentText, "First"))
	assert.Equal(t, map[int]presentationdomain.ActiveModelContent{
		0: {
			Kind: mo.Some(presentationdomain.ModelContentText),
			Text: mo.Some("First"),
		},
		1: {
			Kind: mo.Some(presentationdomain.ModelContentText),
			Text: mo.Some("Hello"),
		},
	}, state.ActiveModel)

	state = service.Apply(state, testModelEndEvent(presentationdomain.ModelResponseContent{
		Kind: presentationdomain.ModelContentText,
		Text: mo.Some("Hello"),
	}))
	// Assert the projected state contains the ordered transcript and leaves host-owned selections unchanged.
	assert.Equal(t, []presentationdomain.Line{{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("Hello"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)

	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolStarted,
		ToolCallID:           mo.Some("call-1"),
		ToolName:             mo.Some("read"),
		Status:               mo.Some("thinking"),
		Text:                 mo.Some("reading"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
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
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolProgress,
		Status:               mo.Some("in_progress"),
		Text:                 mo.Some("working"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
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
	state = service.Apply(state, testToolOutputEvent(presentationdomain.OutputStdout, "content"))
	state = service.Apply(state, testToolOutputEvent(presentationdomain.OutputStderr, "warning"))
	state = service.Apply(state, testToolEndedEvent("read", "completed", false))
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             mo.Some("read"),
		Text:                 mo.None[string](),
		ExitCode:             mo.None[int](),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents: mo.Some([]presentationdomain.Content{{
			Text:      mo.Some("result"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		ErrorText:         mo.None[string](),
		Failure:           mo.Some(false),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             mo.Some("edit"),
		Text:                 mo.None[string](),
		Failure:              mo.Some(true),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents: mo.Some([]presentationdomain.Content{{
			Text:      mo.Some("denied"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("Hello"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineToolStatus,
			ToolName: mo.Some("read"),
			Status:   mo.Some("thinking"),
			Text:     mo.Some("reading"),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineToolStatus,
			ToolName: mo.Some("read"),
			Status:   mo.Some("in_progress"),
			Text:     mo.Some("working"),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineToolStdout,
			ToolName: mo.Some("read"),
			Text:     mo.Some("content"),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineToolStderr,
			ToolName: mo.Some("read"),
			Text:     mo.Some("warning"),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineToolDone,
			ToolName: mo.Some("read"),
			Status:   mo.Some("completed"),
			Text:     mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineToolDone,
			ToolName: mo.Some("read"),
			Text:     mo.Some("result"),
			Status:   mo.None[string](),
			Contents: mo.Some([]presentationdomain.Content{{
				Text:      mo.Some("result"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			}}),
		},
		{
			Kind:     presentationdomain.LineToolError,
			ToolName: mo.Some("edit"),
			Text:     mo.Some("denied"),
			Status:   mo.None[string](),
			Contents: mo.Some([]presentationdomain.Content{{
				Text:      mo.Some("denied"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			}}),
		},
	}, state.Transcript)
}
