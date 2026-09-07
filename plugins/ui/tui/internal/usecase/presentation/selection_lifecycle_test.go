//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateUpdatesOnlyHostConfirmedSelection verifies errors preserve the prior status.
func TestStateUpdatesOnlyHostConfirmedSelection(t *testing.T) {
	t.Parallel()

	// Arrange configured models and an initial host-confirmed selection.
	models := []ConfiguredModel{{
		ProviderID: "openai-codex",
		ModelID:    "gpt",
		Reasoning:  projectionReasoning(ReasoningChoiceLow, ReasoningChoiceHigh),
	}}
	initial := ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: ReasoningChoiceLow,
	}

	// Act by applying host initialization.
	state := (projection{}).Apply(event{
		FailureCode:          "",
		RestoredTranscript:   nil,
		Kind:                 eventInitialization,
		Models:               models,
		ModelSelection:       mo.Some(initial),
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
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})

	// Assert initialization establishes the configured models and selection.
	assert.Equal(t, mo.Some(initial), state.ModelSelection)
	assert.Equal(t, models, state.Models)
	// Act by applying unrelated and selection-confirmation events.
	state = state.Apply(
		testPresentationEvent(eventError, mo.Some("rejected"), mo.None[int]()),
	)
	// Assert the rejected change preserves the host-confirmed selection.
	assert.Equal(t, mo.Some(initial), state.ModelSelection)

	confirmed := ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: ReasoningChoiceHigh,
	}
	state = state.Apply(event{
		FailureCode:          "",
		RestoredTranscript:   nil,
		Kind:                 eventModelSelectionChanged,
		ModelSelection:       mo.Some(confirmed),
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
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})

	// Assert host confirmation updates the selected reasoning choice.
	assert.Equal(t, mo.Some(confirmed), state.ModelSelection)
}

// TestStateReplacesProvisionalToolCallBeforeExecutionStart verifies final arguments replace provisional fields
// before execution.
func TestStateReplacesProvisionalToolCallBeforeExecutionStart(t *testing.T) {
	t.Parallel()

	// Arrange a provisional tool call in presentation state.
	state := (projection{}).Apply(event{
		FailureCode:        "",
		RestoredTranscript: nil,
		Kind:               eventToolCallPreview,
		ToolCall: mo.Some(ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: true,
			Fields: []ToolCallField{{
				Name:   "path",
				Prefix: mo.Some("fi"),
				Value:  mo.None[any](),
			}},
			Arguments: nil,
		}),
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
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	require.True(t, state.ActiveToolCalls["call-1"].Provisional)
	// Act by applying final-call and execution-start events.
	state = state.Apply(event{
		FailureCode:        "",
		RestoredTranscript: nil,
		Kind:               eventToolCallFinal,
		ToolCall: mo.Some(ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: false,
			Arguments:   map[string]any{"path": "file.txt"},
			Fields:      nil,
		}),
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
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	// Assert the final call replaces provisional fields before execution starts.
	require.False(t, state.ActiveToolCalls["call-1"].Provisional)
	state = state.Apply(event{
		FailureCode:          "",
		RestoredTranscript:   nil,
		Kind:                 eventModelEnd,
		Status:               mo.Some("tool_use"),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
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
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	require.Contains(t, state.ActiveToolCalls, "call-1")
	state = state.Apply(event{
		FailureCode:          "",
		RestoredTranscript:   nil,
		Kind:                 eventToolStarted,
		ToolCallID:           mo.Some("call-1"),
		ToolName:             mo.Some("read"),
		Status:               mo.Some("started"),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
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
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	require.Len(t, state.Transcript, 2)
	require.Equal(t, mo.Some("{\"path\":\"file.txt\"}"), state.Transcript[0].Text)
	require.Equal(t, mo.Some("started"), state.Transcript[1].Status)
}
