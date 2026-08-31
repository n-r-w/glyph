//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
	})

	// Assert initialization establishes the configured models and selection.
	assert.Equal(t, mo.Some(initial), state.ModelSelection)
	assert.Equal(t, models, state.Models)
	// Act by applying unrelated and selection-confirmation events.
	state = service.Apply(
		state,
		testPresentationEvent(presentationdomain.EventError, mo.Some("rejected"), mo.None[int]()),
	)
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
	})

	// Assert host confirmation updates the selected reasoning choice.
	assert.Equal(t, mo.Some(confirmed), state.ModelSelection)
}

// TestServiceReplacesProvisionalToolCallBeforeExecutionStart verifies final arguments replace provisional fields
// before execution.
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
	})
	require.Len(t, state.Transcript, 2)
	require.Equal(t, mo.Some("{\"path\":\"file.txt\"}"), state.Transcript[0].Text)
	require.Equal(t, mo.Some("started"), state.Transcript[1].Status)
}
