package presentation

import (
	"testing"

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
				Text:               "Glyph session initialized.",
				ToolName:           "",
				Status:             "",
				ToolResultContents: nil,
			},
			{
				Kind:               presentationdomain.LineError,
				Text:               "Optional extension is unavailable.",
				ToolName:           "",
				Status:             "",
				ToolResultContents: nil,
			},
		},
		Availability: presentationdomain.AvailabilityIdle,
		Extensions: []presentationdomain.Extension{
			{
				ID:    "tools",
				Tools: []string{"read", "edit"},
				Path:  "",
			},
		},
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})

	require.Len(t, state.Startup, 2)
	assert.Equal(t, presentationdomain.Line{
		Kind:               presentationdomain.LineInformation,
		Text:               "Glyph session initialized.",
		ToolName:           "",
		Status:             "",
		ToolResultContents: nil,
	}, state.Startup[0])
	assert.Equal(t, presentationdomain.LineError, state.Startup[1].Kind)
	assert.Equal(t, presentationdomain.AvailabilityIdle, state.Availability)

	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		ModelContentKind:     presentationdomain.ModelContentText,
		Text:                 "Hel",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		ModelContentKind:     presentationdomain.ModelContentText,
		Text:                 "lo",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             0,
		ModelContentKind:     presentationdomain.ModelContentText,
		Text:                 "First",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	assert.Equal(t, map[int]presentationdomain.ActiveModelContent{
		0: {
			Kind: presentationdomain.ModelContentText,
			Text: "First",
		},
		1: {
			Kind: presentationdomain.ModelContentText,
			Text: "Hello",
		},
	}, state.ActiveModel)

	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "Hello",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		Position:           0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})
	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineModel,
		Text:               "Hello",
		ToolName:           "",
		Status:             "",
		ToolResultContents: nil,
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)

	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolStarted,
		ToolCallID:           "call-1",
		ToolName:             "read",
		Status:               "thinking",
		Text:                 "reading",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolProgress,
		Status:               "in_progress",
		Text:                 "working",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolOutput,
		Stream:               presentationdomain.OutputStdout,
		Text:                 "content",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolOutput,
		Stream:               presentationdomain.OutputStderr,
		Text:                 "warning",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolEnded,
		ToolName:             "read",
		Status:               "completed",
		Text:                 "done",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             "read",
		Text:                 "result",
		ExitCode:             0,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             "edit",
		Text:                 "denied",
		Failure:              true,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               "Hello",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolStatus,
			ToolName:           "read",
			Status:             "thinking",
			Text:               "reading",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolStatus,
			ToolName:           "read",
			Status:             "in_progress",
			Text:               "working",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolStdout,
			ToolName:           "read",
			Text:               "content",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolStderr,
			ToolName:           "read",
			Text:               "warning",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolDone,
			ToolName:           "read",
			Status:             "completed",
			Text:               "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolDone,
			ToolName:           "read",
			Text:               "result",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolError,
			ToolName:           "edit",
			Text:               "denied",
			Status:             "",
			ToolResultContents: nil,
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
		ModelSelection:       initial,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
	})

	assert.Equal(t, initial, state.ModelSelection)
	assert.Equal(t, models, state.Models)
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventError,
		Text:                 "rejected",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	assert.Equal(t, initial, state.ModelSelection)

	confirmed := presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
	}
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelSelectionChanged,
		ModelSelection:       confirmed,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
	})
	assert.Equal(t, confirmed, state.ModelSelection)
}

// TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions verifies one complete terminal model line.
func TestServiceReplacesProvisionalToolCallBeforeExecutionStart(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallPreview,
		ToolCall: presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: true,
			Fields: []presentationdomain.ToolCallField{{
				Name:     "path",
				Prefix:   "fi",
				Value:    nil,
				Complete: false,
			}},
			Arguments: nil,
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	require.True(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallFinal,
		ToolCall: presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: false,
			Arguments:   map[string]any{"path": "file.txt"},
			Fields:      nil,
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	require.False(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelEnd,
		Status:               "tool_use",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	require.Contains(t, state.ActiveToolCalls, "call-1")
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolStarted,
		ToolCallID:           "call-1",
		ToolName:             "read",
		Status:               "started",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	require.Len(t, state.Transcript, 2)
	require.Contains(t, state.Transcript[0].Text, "file.txt")
	require.Equal(t, "started", state.Transcript[1].Status)
}

func TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             0,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		Text:                 "complete answer",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:     presentationdomain.EventModelEnd,
		Position: 0,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "complete answer",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,

		ToolCall:       presentationdomain.ToolCallState{},
		Models:         nil,
		ModelSelection: presentationdomain.ModelSelection{},
	})

	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineModel,
		Text:               "complete answer",
		ToolName:           "",
		Status:             "",
		ToolResultContents: nil,
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServicePreservesFinalizedRefusalBlocks verifies mixed public model content keeps its semantic kind.
func TestServicePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             0,
		ModelContentKind:     presentationdomain.ModelContentText,
		Text:                 "draft",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{
			{
				Kind: presentationdomain.ModelContentText,
				Text: "answer",
			},
			{
				Kind: presentationdomain.ModelContentRefusal,
				Text: "cannot help",
			},
		},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		Position:           0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               "answer",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineRefusal,
			Text:               "cannot help",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
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
		Position:             1,
		Text:                 "stale fragment",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelEnd,
		Position:             0,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
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
		ToolName:             "read",
		Status:               "completed",
		Text:                 "result",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             "read",
		Text:                 "result",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineToolDone,
			ToolName:           "read",
			Status:             "completed",
			Text:               "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineToolDone,
			ToolName:           "read",
			Text:               "result",
			Status:             "",
			ToolResultContents: nil,
		},
	}, state.Transcript)
}

// TestServiceRendersOneSafeErrorAcrossTerminalLifecycleEvents verifies layered failures are not duplicated.
func TestServiceRendersOneSafeErrorAcrossTerminalLifecycleEvents(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		Text:                 "partial",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	for _, event := range []presentationdomain.Event{
		{
			Kind:                 presentationdomain.EventModelEnd,
			Failure:              true,
			ErrorText:            "Provider failed.",
			Startup:              nil,
			Extensions:           nil,
			Availability:         0,
			Position:             0,
			ModelContentKind:     0,
			ModelResponseContent: nil,
			ToolCallID:           "",
			ToolName:             "",
			Status:               "",
			Stream:               0,
			Text:                 "",
			ToolResultContents:   nil,
			ExitCode:             0,
			ToolCall:             presentationdomain.ToolCallState{},
			Models:               nil,
			ModelSelection:       presentationdomain.ModelSelection{},
		},
		{
			Kind:                 presentationdomain.EventTurnEnded,
			Failure:              true,
			ErrorText:            "Provider failed.",
			Startup:              nil,
			Extensions:           nil,
			Availability:         0,
			Position:             0,
			ModelContentKind:     0,
			ModelResponseContent: nil,
			ToolCallID:           "",
			ToolName:             "",
			Status:               "",
			Stream:               0,
			Text:                 "",
			ToolResultContents:   nil,
			ExitCode:             0,
			ToolCall:             presentationdomain.ToolCallState{},
			Models:               nil,
			ModelSelection:       presentationdomain.ModelSelection{},
		},
		{
			Kind:                 presentationdomain.EventAgentSettled,
			Failure:              true,
			Text:                 "Provider failed.",
			Startup:              nil,
			Extensions:           nil,
			Availability:         0,
			Position:             0,
			ModelContentKind:     0,
			ModelResponseContent: nil,
			ToolCallID:           "",
			ToolName:             "",
			Status:               "",
			Stream:               0,
			ToolResultContents:   nil,
			ErrorText:            "",
			ExitCode:             0,
			ToolCall:             presentationdomain.ToolCallState{},
			Models:               nil,
			ModelSelection:       presentationdomain.ModelSelection{},
		},
		{
			Kind:                 presentationdomain.EventError,
			Text:                 "Provider failed.",
			Startup:              nil,
			Extensions:           nil,
			Availability:         0,
			Position:             0,
			ModelContentKind:     0,
			ModelResponseContent: nil,
			ToolCallID:           "",
			ToolName:             "",
			Status:               "",
			Stream:               0,
			ToolResultContents:   nil,
			ErrorText:            "",
			ExitCode:             0,
			Failure:              false,
			ToolCall:             presentationdomain.ToolCallState{},
			Models:               nil,
			ModelSelection:       presentationdomain.ModelSelection{},
		},
	} {
		state = service.Apply(state, event)
	}

	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineError,
		Text:               "Provider failed.",
		ToolName:           "",
		Status:             "",
		ToolResultContents: nil,
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
	assert.True(t, state.Settled)
}

// TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn verifies multi-turn transcript continuity.
func TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventInitialization,
		Availability:         presentationdomain.AvailabilityIdle,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         presentationdomain.AvailabilityRunning,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "first response",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		Position:           0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventAgentSettled,
		Text:                 "completed",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         presentationdomain.AvailabilityIdle,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "second response",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		Position:           0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})

	assert.Equal(t, presentationdomain.AvailabilityIdle, state.Availability)
	assert.True(t, state.Settled)
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               "first response",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineModel,
			Text:               "second response",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
	}, state.Transcript)
}

// TestServiceCopiesTypedToolResultImage verifies presentation state owns image bytes.
func TestServiceCopiesTypedToolResultImage(t *testing.T) {
	t.Parallel()

	service := New()
	content := presentationdomain.ToolResultContent{
		MediaType: "image/png",
		Data:      []byte{1, 2, 3},
		Text:      "",
	}
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             "read",
		ToolResultContents:   []presentationdomain.ToolResultContent{content},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ErrorText:            "",

		ExitCode:       0,
		Failure:        false,
		ToolCall:       presentationdomain.ToolCallState{},
		Models:         nil,
		ModelSelection: presentationdomain.ModelSelection{},
	})
	content.Data[0] = 9

	require.Len(t, state.Transcript, 1)
	assert.Equal(t, []byte{1, 2, 3}, state.Transcript[0].ToolResultContents[0].Data)
}

// TestServiceProjectsTypedToolResultTextInOrder verifies readable ordered terminal output.
func TestServiceProjectsTypedToolResultTextInOrder(t *testing.T) {
	t.Parallel()

	state := New().Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:     presentationdomain.EventToolResult,
		ToolName: "read",
		ToolResultContents: []presentationdomain.ToolResultContent{
			{
				Text:      "first",
				MediaType: "",
				Data:      nil,
			},
			{
				MediaType: "image/png",
				Data:      []byte{1, 2, 3},
				Text:      "",
			},
			{
				Text:      "last",
				MediaType: "",
				Data:      nil,
			},
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})

	require.Len(t, state.Transcript, 1)
	assert.Equal(t, "first\n[image: image/png]\nlast", state.Transcript[0].Text)
}

// TestServiceProjectsAuthorizationInformationAndSafeErrors verifies standalone Host frames remain visible.
func TestServiceProjectsAuthorizationInformationAndSafeErrors(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind:                 presentationdomain.EventAuthorization,
		Text:                 "https://example.test/oauth",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventInformation,
		Text:                 "Open the authorization URL.",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventError,
		Text:                 "Authentication failed.",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         presentationdomain.AvailabilityAuthenticationFailed,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})

	assert.Equal(t, "https://example.test/oauth", state.AuthorizationURL)
	assert.Equal(t, presentationdomain.AvailabilityAuthenticationFailed, state.Availability)
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineInformation,
			Text:               "Open the authorization URL.",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineError,
			Text:               "Authentication failed.",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
	}, state.Transcript)
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
