package presentation

import (
	"testing"
	"time"

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
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
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
		Availability: mo.Some(presentationdomain.AvailabilityIdle),
		Extensions: []presentationdomain.Extension{
			{
				ID:    "tools",
				Tools: []string{"read", "edit"},
				Path:  "",
			},
		},
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
	})

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
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
		Text:                 mo.Some("Hel"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
		Text:                 mo.Some("lo"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(0),
		ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
		Text:                 mo.Some("First"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})
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

	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("Hello"),
		}},
		Startup:          nil,
		Extensions:       nil,
		Availability:     mo.None[presentationdomain.Availability](),
		Position:         mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:       mo.None[string](),
		ToolName:         mo.None[string](),
		Status:           mo.None[string](),
		Stream:           mo.None[presentationdomain.OutputStream](),
		Text:             mo.None[string](),
		Contents:         mo.None[[]presentationdomain.Content](),
		ErrorText:        mo.None[string](),
		ExitCode:         mo.None[int](),
		Failure:          mo.None[bool](),
		ToolCall:         mo.None[presentationdomain.ToolCallState](),
		Models:           nil,
		ModelSelection:   mo.None[presentationdomain.ModelSelection](),
		SessionInfo:      mo.None[presentationdomain.SessionInfo](),
		Sessions:         nil,
	})
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolOutput,
		Stream:               mo.Some(presentationdomain.OutputStdout),
		Text:                 mo.Some("content"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolOutput,
		Stream:               mo.Some(presentationdomain.OutputStderr),
		Text:                 mo.Some("warning"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolEnded,
		ToolName:             mo.Some("read"),
		Status:               mo.Some("completed"),
		Text:                 mo.None[string](),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})
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
		ErrorText:      mo.None[string](),
		Failure:        mo.Some(false),
		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](),
		Sessions:       nil,
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
		ErrorText:      mo.None[string](),
		ExitCode:       mo.None[int](),
		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](),
		Sessions:       nil,
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

// TestServiceUpdatesOnlyHostConfirmedSelection verifies errors preserve the prior status.
func TestServiceUpdatesOnlyHostConfirmedSelection(t *testing.T) {
	t.Parallel()

	// Arrange configured models and an initial host-confirmed selection.
	service := New()
	models := []presentationdomain.ConfiguredModel{{
		ProviderID: "openai-codex",
		ModelID:    "gpt",
		Reasoning:  testReasoning(presentationdomain.ReasoningChoiceLow, presentationdomain.ReasoningChoiceHigh),
	}}
	initial := presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceLow,
	}

	// Act by applying host initialization.
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInitialization,
		Models:               models,
		ModelSelection:       mo.Some(initial),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})

	// Assert initialization establishes the configured models and selection.
	assert.Equal(t, mo.Some(initial), state.ModelSelection)
	assert.Equal(t, models, state.Models)
	// Act by applying unrelated and selection-confirmation events.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventError,
		Text:                 mo.Some("rejected"),
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
	})
	// Assert the rejected change preserves the host-confirmed selection.
	assert.Equal(t, mo.Some(initial), state.ModelSelection)

	confirmed := presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
	}
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelSelectionChanged,
		ModelSelection:       mo.Some(confirmed),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})

	// Assert host confirmation updates the selected reasoning choice.
	assert.Equal(t, mo.Some(confirmed), state.ModelSelection)
}

// TestServiceReplacesProvisionalToolCallBeforeExecutionStart verifies final arguments replace provisional fields before execution.
func TestServiceReplacesProvisionalToolCallBeforeExecutionStart(t *testing.T) {
	t.Parallel()

	// Arrange a provisional tool call in presentation state.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventToolCallPreview,
		ToolCall: mo.Some(presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: true,
			Fields: []presentationdomain.ToolCallField{{
				Name:   "path",
				Prefix: mo.Some("fi"),
				Value:  mo.None[any](),
			}},
			Arguments: nil,
		}),
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
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})
	require.True(t, state.ActiveToolCalls["call-1"].Provisional)
	// Act by applying final-call and execution-start events.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventToolCallFinal,
		ToolCall: mo.Some(presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: false,
			Arguments:   map[string]any{"path": "file.txt"},
			Fields:      nil,
		}),
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
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})
	// Assert the final call replaces provisional fields before execution starts.
	require.False(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelEnd,
		Status:               mo.Some("tool_use"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
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
	})
	require.Contains(t, state.ActiveToolCalls, "call-1")
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolStarted,
		ToolCallID:           mo.Some("call-1"),
		ToolName:             mo.Some("read"),
		Status:               mo.Some("started"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
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
	})
	require.Len(t, state.Transcript, 2)
	require.Equal(t, mo.Some("{\"path\":\"file.txt\"}"), state.Transcript[0].Text)
	require.Equal(t, mo.Some("started"), state.Transcript[1].Status)
}

// TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions verifies terminal content merges deltas from distinct stream positions.
func TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions(t *testing.T) {
	t.Parallel()

	// Arrange model deltas that occupy distinct stream positions.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(0),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})
	// Act by applying later deltas and the terminal model response.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("complete answer"),
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		Position:           mo.None[int](),
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("complete answer"),
		}},
		Startup:          nil,
		Extensions:       nil,
		Availability:     mo.None[presentationdomain.Availability](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:       mo.None[string](),
		ToolName:         mo.None[string](),
		Status:           mo.None[string](),
		Stream:           mo.None[presentationdomain.OutputStream](),
		Text:             mo.None[string](),
		Contents:         mo.None[[]presentationdomain.Content](),
		ErrorText:        mo.None[string](),
		ExitCode:         mo.None[int](),
		Failure:          mo.None[bool](),

		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](),
		Sessions:       nil,
	})

	// Assert the finalized transcript contains one complete ordered model message.
	assert.Equal(t, []presentationdomain.Line{{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("complete answer"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServicePreservesFinalizedRefusalBlocks verifies mixed public model content keeps its semantic kind.
func TestServicePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	// Arrange a streamed refusal followed by terminal response content.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(0),
		ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
		Text:                 mo.Some("draft"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})
	// Act by applying the model-end event.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{
			{
				Kind: presentationdomain.ModelContentText,
				Text: mo.Some("answer"),
			},
			{
				Kind: presentationdomain.ModelContentRefusal,
				Text: mo.Some("cannot help"),
			},
		},
		Startup:          nil,
		Extensions:       nil,
		Availability:     mo.None[presentationdomain.Availability](),
		Position:         mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:       mo.None[string](),
		ToolName:         mo.None[string](),
		Status:           mo.None[string](),
		Stream:           mo.None[presentationdomain.OutputStream](),
		Text:             mo.None[string](),
		Contents:         mo.None[[]presentationdomain.Content](),
		ErrorText:        mo.None[string](),
		ExitCode:         mo.None[int](),
		Failure:          mo.None[bool](),
		ToolCall:         mo.None[presentationdomain.ToolCallState](),
		Models:           nil,
		ModelSelection:   mo.None[presentationdomain.ModelSelection](),
		SessionInfo:      mo.None[presentationdomain.SessionInfo](),
		Sessions:         nil,
	})

	// Assert the transcript retains the refusal kind and text.
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("answer"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineRefusal,
			Text:     mo.Some("cannot help"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine verifies tool-only model cleanup.
func TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine(t *testing.T) {
	t.Parallel()

	// Arrange an active model fragment with no terminal content.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("stale fragment"),
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
	})
	// Act by applying an empty model-end event.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelEnd,
		Position:             mo.None[int](),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})

	// Assert stale fragments are cleared without adding a transcript line.
	assert.Empty(t, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceAssignsToolCompletionStatusAndResultContentOnce verifies distinct terminal payload owners.
func TestServiceAssignsToolCompletionStatusAndResultContentOnce(t *testing.T) {
	t.Parallel()

	// Arrange a running tool call and its typed result content.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolEnded,
		ToolName:             mo.Some("read"),
		Status:               mo.Some("completed"),
		Text:                 mo.None[string](),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	})
	// Act by applying the terminal tool-result event.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             mo.Some("read"),
		Text:                 mo.None[string](),
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
		ErrorText:      mo.None[string](),
		ExitCode:       mo.None[int](),
		Failure:        mo.Some(false),
		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](),
		Sessions:       nil,
	})

	// Assert the tool is finalized once with status and result content.
	assert.Equal(t, []presentationdomain.Line{
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
	}, state.Transcript)
}

// TestServiceRendersOneSafeErrorAcrossTerminalLifecycleEvents verifies layered failures are not duplicated.
func TestServiceRendersOneSafeErrorAcrossTerminalLifecycleEvents(t *testing.T) {
	t.Parallel()

	// Arrange terminal lifecycle events that carry the same safe error.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("partial"),
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
	})
	// Act by applying every terminal event.
	for _, event := range []presentationdomain.Event{
		{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventModelEnd,
			Failure:              mo.Some(true),
			ErrorText:            mo.Some("Provider failed."),
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
			ExitCode:             mo.None[int](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo: mo.None[presentationdomain.
				SessionInfo](),
			Sessions: nil,
		},
		{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventTurnEnded,
			Failure:              mo.Some(true),
			ErrorText:            mo.Some("Provider failed."),
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
			ExitCode:             mo.None[int](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo: mo.None[presentationdomain.
				SessionInfo](),
			Sessions: nil,
		},
		{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventAgentSettled,
			Failure:              mo.Some(true),
			Text:                 mo.Some("Provider failed."),
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
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo: mo.None[presentationdomain.
				SessionInfo](),
			Sessions: nil,
		},
		{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventError,
			Text:                 mo.Some("Provider failed."),
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
			SessionInfo: mo.None[presentationdomain.
				SessionInfo](),
			Sessions: nil,
		},
	} {
		state = service.Apply(state, event)
	}

	// Assert the transcript contains one safe error line.
	assert.Equal(t, []presentationdomain.Line{{
		Kind:     presentationdomain.LineError,
		Text:     mo.Some("Provider failed."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
	assert.Equal(t, mo.Some(true), state.Settled)
}

// TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn verifies multi-turn transcript continuity.
func TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn(t *testing.T) {
	t.Parallel()

	// Arrange a completed first turn and a second active turn.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInitialization,
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
	})
	// Act by settling the first turn and applying the second turn.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityRunning),
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("first response"),
		}},
		Startup:          nil,
		Extensions:       nil,
		Availability:     mo.None[presentationdomain.Availability](),
		Position:         mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:       mo.None[string](),
		ToolName:         mo.None[string](),
		Status:           mo.None[string](),
		Stream:           mo.None[presentationdomain.OutputStream](),
		Text:             mo.None[string](),
		Contents:         mo.None[[]presentationdomain.Content](),
		ErrorText:        mo.None[string](),
		ExitCode:         mo.None[int](),
		Failure:          mo.None[bool](),
		ToolCall:         mo.None[presentationdomain.ToolCallState](),
		Models:           nil,
		ModelSelection:   mo.None[presentationdomain.ModelSelection](),
		SessionInfo:      mo.None[presentationdomain.SessionInfo](),
		Sessions:         nil,
	})
	state = service.Apply(state, presentationdomain.Event{
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
	})
	state = service.Apply(state, presentationdomain.Event{
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("second response"),
		}},
		Startup:          nil,
		Extensions:       nil,
		Availability:     mo.None[presentationdomain.Availability](),
		Position:         mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:       mo.None[string](),
		ToolName:         mo.None[string](),
		Status:           mo.None[string](),
		Stream:           mo.None[presentationdomain.OutputStream](),
		Text:             mo.None[string](),
		Contents:         mo.None[[]presentationdomain.Content](),
		ErrorText:        mo.None[string](),
		ExitCode:         mo.None[int](),
		Failure:          mo.None[bool](),
		ToolCall:         mo.None[presentationdomain.ToolCallState](),
		Models:           nil,
		ModelSelection:   mo.None[presentationdomain.ModelSelection](),
		SessionInfo:      mo.None[presentationdomain.SessionInfo](),
		Sessions:         nil,
	})

	// Assert settlement state and both turns remain projected in order.
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)
	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("first response"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("second response"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}, state.Transcript)
}

// TestServiceCopiesTypedToolResultImage verifies presentation state owns image bytes.
func TestServiceCopiesTypedToolResultImage(t *testing.T) {
	t.Parallel()

	// Arrange typed tool-result image content with caller-owned bytes.
	service := New()
	content := presentationdomain.Content{
		MediaType: mo.Some("image/png"),
		Data:      mo.Some([]byte{1, 2, 3}),
		Text:      mo.None[string](),
	}
	// Act by applying the result and mutating the caller-owned bytes.
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             mo.Some("read"),
		Contents:             mo.Some([]presentationdomain.Content{content}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		ErrorText:            mo.None[string](),

		ExitCode:       mo.None[int](),
		Failure:        mo.Some(false),
		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](),
		Sessions:       nil,
	})
	data, ok := content.Data.Get()
	require.True(t, ok)
	data[0] = 9

	// Assert the stored transcript retains independently owned image bytes.
	require.Len(t, state.Transcript, 1)
	contents, ok := state.Transcript[0].Contents.Get()
	require.True(t, ok)
	clonedData, ok := contents[0].Data.Get()
	require.True(t, ok)
	assert.Equal(t, []byte{1, 2, 3}, clonedData)
}

// TestServiceClonesContentsAcrossStateSnapshots verifies image content cannot alias earlier presentation state.
func TestServiceClonesContentsAcrossStateSnapshots(t *testing.T) {
	t.Parallel()

	// Arrange startup and transcript lines that share one image payload.
	line := presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("read"),
		Status:   mo.None[string](),
		Text:     mo.Some("[image: image/png]"),
		Contents: mo.Some([]presentationdomain.Content{{
			Text:      mo.None[string](),
			MediaType: mo.Some("image/png"),
			Data:      mo.Some([]byte{1, 2, 3}),
		}}),
	}
	previous := presentationdomain.State{
		Startup:          []presentationdomain.Line{line},
		Transcript:       []presentationdomain.Line{line},
		Models:           nil,
		ActiveModel:      nil,
		ActiveToolCalls:  nil,
		ActiveTools:      nil,
		Availability:     mo.None[presentationdomain.Availability](),
		AuthorizationURL: mo.None[string](),
		Settled:          mo.None[bool](),
		ModelSelection:   mo.None[presentationdomain.ModelSelection](),
		SessionInfo:      mo.None[presentationdomain.SessionInfo](),
		Sessions:         nil,
	}
	// Act by applying an event and mutating both image copies in the next state.
	next := New().Apply(previous, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventTurnStarted,
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
		Sessions:             nil,
	})

	for _, lines := range [][]presentationdomain.Line{next.Startup, next.Transcript} {
		contents, ok := lines[0].Contents.Get()
		require.True(t, ok)
		contents[0].MediaType = mo.Some("image/jpeg")
		data, ok := contents[0].Data.Get()
		require.True(t, ok)
		data[0] = 9
	}

	// Assert the previous state retains its original media type and bytes.
	for _, lines := range [][]presentationdomain.Line{previous.Startup, previous.Transcript} {
		contents, ok := lines[0].Contents.Get()
		require.True(t, ok)
		assert.Equal(t, mo.Some("image/png"), contents[0].MediaType)
		data, ok := contents[0].Data.Get()
		require.True(t, ok)
		assert.Equal(t, []byte{1, 2, 3}, data)
	}
}

// TestServiceProjectsTypedToolResultTextInOrder verifies readable ordered terminal output.
func TestServiceProjectsTypedToolResultTextInOrder(t *testing.T) {
	t.Parallel()

	// Arrange an event with ordered text and image tool-result content.
	event := presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventToolResult,
		ToolName:           mo.Some("read"),
		Contents: mo.Some([]presentationdomain.Content{
			{
				Text:      mo.Some("first"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			},
			{
				MediaType: mo.Some("image/png"),
				Data:      mo.Some([]byte{1, 2, 3}),
				Text:      mo.None[string](),
			},
			{
				Text:      mo.Some("last"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			},
		}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	}

	// Act by applying the typed tool result.
	state := New().Apply(presentationdomain.State{}, event)

	// Assert the transcript text projection preserves content order.
	require.Len(t, state.Transcript, 1)
	assert.Equal(t, mo.Some("first\n[image: image/png]\nlast"), state.Transcript[0].Text)
}

// TestServiceProjectsAuthorizationInformationAndSafeErrors verifies standalone Host frames remain visible.
func TestServiceProjectsAuthorizationInformationAndSafeErrors(t *testing.T) {
	t.Parallel()

	// Arrange authorization, information, and safe-error lifecycle events.
	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAuthorization,
		Text:                 mo.Some("https://example.test/oauth"),
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
	})
	// Act by applying the information and safe-error events.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInformation,
		Text:                 mo.Some("Open the authorization URL."),
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventError,
		Text:                 mo.Some("Authentication failed."),
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
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityAuthenticationFailed),
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
	})

	// Assert authorization state and safe transcript lines are projected.
	assert.Equal(t, mo.Some("https://example.test/oauth"), state.AuthorizationURL)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityAuthenticationFailed), state.Availability)
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineInformation,
			Text:     mo.Some("Open the authorization URL."),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineError,
			Text:     mo.Some("Authentication failed."),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}, state.Transcript)
}

// TestServicePreservesAbsentStateAndCopiesOptionalJSON verifies None state and mutable Some payload isolation.
func TestServicePreservesAbsentStateAndCopiesOptionalJSON(t *testing.T) {
	t.Parallel()

	// Arrange absent optional state and nested caller-owned JSON values.
	value := map[string]any{
		"nested": []any{[]byte{1, 2, 3}},
	}
	state := New().Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolCallPreview,
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
		ToolCall: mo.Some(presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    0,
			Provisional: true,
			Fields: []presentationdomain.ToolCallField{{
				Name:   "value",
				Value:  mo.Some[any](value),
				Prefix: mo.None[string](),
			}},
			Arguments: nil,
		}),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](),
		Sessions:       nil,
	})

	value["nested"].([]any)[0].([]byte)[0] = 9
	clonedValue, ok := state.ActiveToolCalls["call-1"].Fields[0].Value.Get()
	require.True(t, ok)
	assert.Equal(t, byte(1), clonedValue.(map[string]any)["nested"].([]any)[0].([]byte)[0])
	assert.True(t, state.Availability.IsNone())
	assert.True(t, state.AuthorizationURL.IsNone())
	assert.True(t, state.Settled.IsNone())
	assert.True(t, state.ModelSelection.IsNone())

	// Act by applying content with absent text and optional JSON.
	state = New().Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.Some(0),
		ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
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
	})
	// Assert absence is preserved and nested JSON is independently owned.
	assert.Equal(t, mo.Some(presentationdomain.ModelContentText), state.ActiveModel[0].Kind)
	assert.True(t, state.ActiveModel[0].Text.IsNone())

	state = New().Apply(state, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventTurnStarted,
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
		Sessions:             nil,
	})
	assert.Equal(t, mo.Some(false), state.Settled)
}

// TestServiceIgnoresMissingSelectedPayload verifies malformed events do not project zero payloads.
func TestServiceIgnoresMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	// Arrange an information event without any selected payload.
	event := presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInformation,
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
		Sessions:             nil,
	}

	// Act by applying the incomplete information event.
	state := New().Apply(presentationdomain.State{}, event)

	// Assert the incomplete event adds no transcript content.
	assert.Empty(t, state.Transcript)
}

// TestServiceReplacesRestoredTranscriptOnlyAfterConfirmedSessionChange verifies replacement waits for session identity.
func TestServiceReplacesRestoredTranscriptOnlyAfterConfirmedSessionChange(t *testing.T) {
	t.Parallel()

	// Arrange an existing transcript, restored entries, and pending and confirmed session events.
	oldLine := textLine(presentationdomain.LineUser, mo.Some("old"))
	restored := []presentationdomain.Line{
		textLine(presentationdomain.LineUser, mo.Some("prior-user")),
		textLine(presentationdomain.LineModel, mo.Some("prior-model")),
	}
	service := New()
	state := presentationdomain.State{
		Startup: nil, Transcript: []presentationdomain.Line{oldLine}, Models: nil,
		ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		Availability: mo.None[presentationdomain.Availability](), Settled: mo.None[bool](),
		AuthorizationURL: mo.None[string](), ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
	}
	// Act by applying a pending replacement before session identity is confirmed.
	pending := testSessionEvent(presentationdomain.EventSessionChanged, mo.None[presentationdomain.SessionInfo](), restored)
	state = service.Apply(state, pending)

	// Assert the pending event retains the existing transcript.

	require.Equal(t, []presentationdomain.Line{oldLine}, state.Transcript)

	timestamp := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	info := presentationdomain.SessionInfo{
		ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	// Act by applying the confirmed replacement twice.
	confirmed := testSessionEvent(presentationdomain.EventSessionChanged, mo.Some(info), restored)
	state = service.Apply(state, confirmed)

	// Assert confirmation replaces the transcript and repeated delivery is idempotent.
	require.Equal(t, restored, state.Transcript)
	state = service.Apply(state, confirmed)
	require.Equal(t, restored, state.Transcript)

	information := testSessionEvent(presentationdomain.EventSessionInformation, mo.Some(info), []presentationdomain.Line{oldLine})
	state = service.Apply(state, information)
	require.Equal(t, restored, state.Transcript)
}

// TestServiceOwnsRestoredUserImageBytes verifies restored user images transfer ownership to presentation state.
func TestServiceOwnsRestoredUserImageBytes(t *testing.T) {
	t.Parallel()

	// Arrange a restored user line backed by caller-owned image bytes.
	imageBytes := []byte{1, 2, 3}
	restored := []presentationdomain.Line{{
		Kind: presentationdomain.LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
		Text: mo.Some("[image image/png, 3 bytes]"),
		Contents: mo.Some([]presentationdomain.Content{{
			Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some(imageBytes),
		}}),
	}}
	service := New()
	state := presentationdomain.State{
		Startup: nil, Transcript: nil, Models: nil, ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		Availability: mo.None[presentationdomain.Availability](), AuthorizationURL: mo.None[string](),
		Settled: mo.None[bool](), ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
	}
	timestamp := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	info := presentationdomain.SessionInfo{
		ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: timestamp, UpdatedAt: timestamp,
	}

	// Act by applying SessionChanged and mutating every caller-owned byte reference.
	state = service.Apply(state, testSessionEvent(
		presentationdomain.EventSessionChanged, mo.Some(info), restored,
	))
	imageBytes[0] = 9
	restored[0].Contents.MustGet()[0].Data.MustGet()[1] = 9

	// Assert presentation state retains an independent copy of the original image.
	require.Equal(t, []byte{1, 2, 3}, state.Transcript[0].Contents.MustGet()[0].Data.MustGet())
}

func testSessionEvent(
	kind presentationdomain.EventKind,
	info mo.Option[presentationdomain.SessionInfo],
	restored []presentationdomain.Line,
) presentationdomain.Event {
	return presentationdomain.Event{
		Kind: kind, Startup: nil, RestoredTranscript: restored, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](), SessionInfo: info, Sessions: nil,
	}
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
