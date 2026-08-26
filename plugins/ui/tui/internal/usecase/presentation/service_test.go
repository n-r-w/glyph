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

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
			{
				Kind:               presentationdomain.LineInformation,
				Text:               mo.Some("Glyph session initialized."),
				ToolName:           mo.None[string](),
				Status:             mo.None[string](),
				ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
			},
			{
				Kind:               presentationdomain.LineError,
				Text:               mo.Some("Optional extension is unavailable."),
				ToolName:           mo.None[string](),
				Status:             mo.None[string](),
				ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})

	require.Len(t, state.Startup, 2)
	assert.Equal(t, presentationdomain.Line{
		Kind:               presentationdomain.LineInformation,
		Text:               mo.Some("Glyph session initialized."),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	}, state.Startup[0])
	assert.Equal(t, presentationdomain.LineError, state.Startup[1].Kind)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)

	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
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
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("Hello"),
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       mo.None[presentationdomain.Availability](),
		Position:           mo.None[int](),
		ModelContentKind:   mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		Stream:             mo.None[presentationdomain.OutputStream](),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:          mo.None[string](),
		ExitCode:           mo.None[int](),
		Failure:            mo.None[bool](),
		ToolCall:           mo.None[presentationdomain.ToolCallState](),
		Models:             nil,
		ModelSelection:     mo.None[presentationdomain.ModelSelection](),
	})
	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineModel,
		Text:               mo.Some("Hello"),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)

	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
			Text:      mo.Some("result"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		ErrorText:      mo.None[string](),
		Failure:        mo.Some(false),
		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
			Text:      mo.Some("denied"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		ErrorText:      mo.None[string](),
		ExitCode:       mo.None[int](),
		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               mo.Some("Hello"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineToolStatus,
			ToolName:           mo.Some("read"),
			Status:             mo.Some("thinking"),
			Text:               mo.Some("reading"),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineToolStatus,
			ToolName:           mo.Some("read"),
			Status:             mo.Some("in_progress"),
			Text:               mo.Some("working"),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineToolStdout,
			ToolName:           mo.Some("read"),
			Text:               mo.Some("content"),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineToolStderr,
			ToolName:           mo.Some("read"),
			Text:               mo.Some("warning"),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineToolDone,
			ToolName:           mo.Some("read"),
			Status:             mo.Some("completed"),
			Text:               mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:     presentationdomain.LineToolDone,
			ToolName: mo.Some("read"),
			Text:     mo.Some("result"),
			Status:   mo.None[string](),
			ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
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
			ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
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
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
	})

	assert.Equal(t, mo.Some(initial), state.ModelSelection)
	assert.Equal(t, models, state.Models)
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	assert.Equal(t, mo.Some(initial), state.ModelSelection)

	confirmed := presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
	}
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
	})
	assert.Equal(t, mo.Some(confirmed), state.ModelSelection)
}

// TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions verifies one complete terminal model line.
func TestServiceReplacesProvisionalToolCallBeforeExecutionStart(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallPreview,
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	require.True(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallFinal,
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	require.False(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	require.Contains(t, state.ActiveToolCalls, "call-1")
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	require.Len(t, state.Transcript, 2)
	require.Equal(t, mo.Some("{\"path\":\"file.txt\"}"), state.Transcript[0].Text)
	require.Equal(t, mo.Some("started"), state.Transcript[1].Status)
}

func TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:     presentationdomain.EventModelEnd,
		Position: mo.None[int](),
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("complete answer"),
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       mo.None[presentationdomain.Availability](),
		ModelContentKind:   mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		Stream:             mo.None[presentationdomain.OutputStream](),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:          mo.None[string](),
		ExitCode:           mo.None[int](),
		Failure:            mo.None[bool](),

		ToolCall:       mo.None[presentationdomain.ToolCallState](),
		Models:         nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
	})

	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineModel,
		Text:               mo.Some("complete answer"),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServicePreservesFinalizedRefusalBlocks verifies mixed public model content keeps its semantic kind.
func TestServicePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
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
		Startup:            nil,
		Extensions:         nil,
		Availability:       mo.None[presentationdomain.Availability](),
		Position:           mo.None[int](),
		ModelContentKind:   mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		Stream:             mo.None[presentationdomain.OutputStream](),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:          mo.None[string](),
		ExitCode:           mo.None[int](),
		Failure:            mo.None[bool](),
		ToolCall:           mo.None[presentationdomain.ToolCallState](),
		Models:             nil,
		ModelSelection:     mo.None[presentationdomain.ModelSelection](),
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               mo.Some("answer"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineRefusal,
			Text:               mo.Some("cannot help"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine verifies tool-only model cleanup.
func TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})

	assert.Empty(t, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceAssignsToolCompletionStatusAndResultContentOnce verifies distinct terminal payload owners.
func TestServiceAssignsToolCompletionStatusAndResultContentOnce(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
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
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineToolDone,
			ToolName:           mo.Some("read"),
			Status:             mo.Some("completed"),
			Text:               mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:     presentationdomain.LineToolDone,
			ToolName: mo.Some("read"),
			Text:     mo.Some("result"),
			Status:   mo.None[string](),
			ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
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

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	for _, event := range []presentationdomain.Event{
		{
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ExitCode:             mo.None[int](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		},
		{
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ExitCode:             mo.None[int](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		},
		{
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		},
		{
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		},
	} {
		state = service.Apply(state, event)
	}

	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineError,
		Text:               mo.Some("Provider failed."),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
	assert.Equal(t, mo.Some(true), state.Settled)
}

// TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn verifies multi-turn transcript continuity.
func TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("first response"),
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       mo.None[presentationdomain.Availability](),
		Position:           mo.None[int](),
		ModelContentKind:   mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		Stream:             mo.None[presentationdomain.OutputStream](),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:          mo.None[string](),
		ExitCode:           mo.None[int](),
		Failure:            mo.None[bool](),
		ToolCall:           mo.None[presentationdomain.ToolCallState](),
		Models:             nil,
		ModelSelection:     mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("second response"),
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       mo.None[presentationdomain.Availability](),
		Position:           mo.None[int](),
		ModelContentKind:   mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		Stream:             mo.None[presentationdomain.OutputStream](),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:          mo.None[string](),
		ExitCode:           mo.None[int](),
		Failure:            mo.None[bool](),
		ToolCall:           mo.None[presentationdomain.ToolCallState](),
		Models:             nil,
		ModelSelection:     mo.None[presentationdomain.ModelSelection](),
	})

	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)
	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               mo.Some("first response"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineModel,
			Text:               mo.Some("second response"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
	}, state.Transcript)
}

// TestServiceCopiesTypedToolResultImage verifies presentation state owns image bytes.
func TestServiceCopiesTypedToolResultImage(t *testing.T) {
	t.Parallel()

	service := New()
	content := presentationdomain.ToolResultContent{
		MediaType: mo.Some("image/png"),
		Data:      mo.Some([]byte{1, 2, 3}),
		Text:      mo.None[string](),
	}
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             mo.Some("read"),
		ToolResultContents:   mo.Some([]presentationdomain.ToolResultContent{content}),
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
	})
	data, ok := content.Data.Get()
	require.True(t, ok)
	data[0] = 9

	require.Len(t, state.Transcript, 1)
	contents, ok := state.Transcript[0].ToolResultContents.Get()
	require.True(t, ok)
	clonedData, ok := contents[0].Data.Get()
	require.True(t, ok)
	assert.Equal(t, []byte{1, 2, 3}, clonedData)
}

// TestServiceProjectsTypedToolResultTextInOrder verifies readable ordered terminal output.
func TestServiceProjectsTypedToolResultTextInOrder(t *testing.T) {
	t.Parallel()

	state := New().Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:     presentationdomain.EventToolResult,
		ToolName: mo.Some("read"),
		ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{
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
	})

	require.Len(t, state.Transcript, 1)
	assert.Equal(t, mo.Some("first\n[image: image/png]\nlast"), state.Transcript[0].Text)
}

// TestServiceProjectsAuthorizationInformationAndSafeErrors verifies standalone Host frames remain visible.
func TestServiceProjectsAuthorizationInformationAndSafeErrors(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})

	assert.Equal(t, mo.Some("https://example.test/oauth"), state.AuthorizationURL)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityAuthenticationFailed), state.Availability)
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineInformation,
			Text:               mo.Some("Open the authorization URL."),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineError,
			Text:               mo.Some("Authentication failed."),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
	}, state.Transcript)
}

// TestServicePreservesAbsentStateAndCopiesOptionalJSON verifies None state and mutable Some payload isolation.
func TestServicePreservesAbsentStateAndCopiesOptionalJSON(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"nested": []any{[]byte{1, 2, 3}},
	}
	state := New().Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
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
	})

	value["nested"].([]any)[0].([]byte)[0] = 9
	clonedValue, ok := state.ActiveToolCalls["call-1"].Fields[0].Value.Get()
	require.True(t, ok)
	assert.Equal(t, byte(1), clonedValue.(map[string]any)["nested"].([]any)[0].([]byte)[0])
	assert.True(t, state.Availability.IsNone())
	assert.True(t, state.AuthorizationURL.IsNone())
	assert.True(t, state.Settled.IsNone())
	assert.True(t, state.ModelSelection.IsNone())

	state = New().Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	assert.Equal(t, mo.Some(presentationdomain.ModelContentText), state.ActiveModel[0].Kind)
	assert.True(t, state.ActiveModel[0].Text.IsNone())

	state = New().Apply(state, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	assert.Equal(t, mo.Some(false), state.Settled)
}

// TestServiceIgnoresMissingSelectedPayload verifies malformed events do not project zero payloads.
func TestServiceIgnoresMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	state := New().Apply(presentationdomain.State{}, presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})

	assert.Empty(t, state.Transcript)
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
