//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateAppliesInitializationAndLifecycleWithoutOwningHostState verifies ordered projection only.
func TestStateAppliesInitializationAndLifecycleWithoutOwningHostState(t *testing.T) {
	t.Parallel()

	// Arrange a complete initialization state.
	state := (projection{}).Apply(testInitializationEvent(
		[]Line{
			{
				Kind:     LineInformation,
				Text:     mo.Some("Glyph session initialized."),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]Content](),
			},
			{
				Kind:     LineError,
				Text:     mo.Some("Optional extension is unavailable."),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]Content](),
			},
		},
		AvailabilityIdle,
	))

	require.Len(t, state.Startup, 2)
	assert.Equal(t, Line{
		Kind:     LineInformation,
		Text:     mo.Some("Glyph session initialized."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]Content](),
	}, state.Startup[0])
	assert.Equal(t, LineError, state.Startup[1].Kind)
	assert.Equal(t, mo.Some(AvailabilityIdle), state.Availability)

	// Act by applying model, tool, error, authorization, and settlement lifecycle events.
	state = state.Apply(testModelDeltaEvent(1, ModelContentText, "Hel"))
	state = state.Apply(testModelDeltaEvent(1, ModelContentText, "lo"))
	state = state.Apply(testModelDeltaEvent(0, ModelContentText, "First"))
	assert.Equal(t, map[int]ActiveModelContent{
		0: {
			Kind: mo.Some(ModelContentText),
			Text: mo.Some("First"),
		},
		1: {
			Kind: mo.Some(ModelContentText),
			Text: mo.Some("Hello"),
		},
	}, state.ActiveModel)

	state = state.Apply(testModelEndEvent(ModelResponseContent{
		Kind: ModelContentText,
		Text: mo.Some("Hello"),
	}))
	// Assert the projected state contains the ordered transcript and leaves host-owned selections unchanged.
	assert.Equal(t, []Line{{
		Kind:     LineModel,
		Text:     mo.Some("Hello"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)

	state = state.Apply(event{
		RestoredTranscript:   nil,
		Kind:                 eventToolStarted,
		ToolCallID:           mo.Some("call-1"),
		ToolName:             mo.Some("read"),
		Status:               mo.Some("thinking"),
		Text:                 mo.Some("reading"),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		Stream:               mo.None[OutputStream](),
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
	state = state.Apply(event{
		RestoredTranscript:   nil,
		Kind:                 eventToolProgress,
		Status:               mo.Some("in_progress"),
		Text:                 mo.Some("working"),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Stream:               mo.None[OutputStream](),
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
	state = state.Apply(testToolOutputEvent(OutputStdout, "content"))
	state = state.Apply(testToolOutputEvent(OutputStderr, "warning"))
	state = state.Apply(testToolEndedEvent("read", "completed", false))
	state = state.Apply(event{
		RestoredTranscript:   nil,
		Kind:                 eventToolResult,
		ToolName:             mo.Some("read"),
		Text:                 mo.None[string](),
		ExitCode:             mo.None[int](),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Contents: mo.Some([]Content{{
			Text:      mo.Some("result"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		ErrorText:         mo.None[string](),
		Failure:           mo.Some(false),
		ToolCall:          mo.None[ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[ModelSelection](),
		SessionInfo:       mo.None[SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
	})
	state = state.Apply(event{
		RestoredTranscript:   nil,
		Kind:                 eventToolResult,
		ToolName:             mo.Some("edit"),
		Text:                 mo.None[string](),
		Failure:              mo.Some(true),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Contents: mo.Some([]Content{{
			Text:      mo.Some("denied"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		ToolCall:          mo.None[ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[ModelSelection](),
		SessionInfo:       mo.None[SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
	})

	assert.Equal(t, []Line{
		{
			Kind:     LineModel,
			Text:     mo.Some("Hello"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineToolStatus,
			ToolName: mo.Some("read"),
			Status:   mo.Some("thinking"),
			Text:     mo.Some("reading"),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineToolStatus,
			ToolName: mo.Some("read"),
			Status:   mo.Some("in_progress"),
			Text:     mo.Some("working"),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineToolStdout,
			ToolName: mo.Some("read"),
			Text:     mo.Some("content"),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineToolStderr,
			ToolName: mo.Some("read"),
			Text:     mo.Some("warning"),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineToolDone,
			ToolName: mo.Some("read"),
			Status:   mo.Some("completed"),
			Text:     mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineToolDone,
			ToolName: mo.Some("read"),
			Text:     mo.Some("result"),
			Status:   mo.None[string](),
			Contents: mo.Some([]Content{{
				Text:      mo.Some("result"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			}}),
		},
		{
			Kind:     LineToolError,
			ToolName: mo.Some("edit"),
			Text:     mo.Some("denied"),
			Status:   mo.None[string](),
			Contents: mo.Some([]Content{{
				Text:      mo.Some("denied"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			}}),
		},
	}, state.Transcript)
}
