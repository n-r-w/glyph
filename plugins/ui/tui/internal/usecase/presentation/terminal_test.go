package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
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
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.Some(false),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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

// TestServiceClearsUnconfirmedModelOnlyOnPersistenceFailure verifies contextual persistence detection.
func TestServiceClearsUnconfirmedModelOnlyOnPersistenceFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		errorText   string
		clearsState bool
	}{
		{
			name:        "persistence error with independent sibling cause",
			errorText:   "session persistence failed: disk full\nprovider request failed",
			clearsState: true,
		},
		{
			name:        "provider error containing persistence phrase",
			errorText:   "provider request failed: session persistence failed upstream",
			clearsState: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one confirmed user line and one streamed model fragment without a terminal model event.
			service := New()
			state := service.Apply(presentationdomain.State{}, testPresentationEvent(
				presentationdomain.EventUserSubmitted,
				mo.Some("durable user"),
				mo.None[int](),
			))
			state = service.Apply(state, testPresentationEvent(
				presentationdomain.EventModelDelta,
				mo.Some("unconfirmed model"),
				mo.Some(0),
			))
			state.ActiveToolCalls = map[string]presentationdomain.ToolCallState{
				"call-1": {
					CallID: "call-1", Name: "bash", Position: 0, Provisional: true,
					Fields: nil, Arguments: nil,
				},
			}
			state.ActiveTools = map[string]string{"call-1": "running"}

			// Act by applying a Host error after the streamed model and tool state.
			state = service.Apply(state, testPresentationEvent(
				presentationdomain.EventError,
				mo.Some(test.errorText),
				mo.None[int](),
			))

			// Assert only persistence failures discard unconfirmed model and tool state.
			if test.clearsState {
				assert.Empty(t, state.ActiveModel)
				assert.Empty(t, state.ActiveToolCalls)
				assert.Empty(t, state.ActiveTools)
			} else {
				require.Contains(t, state.ActiveModel, 0)
				assert.Equal(t, "unconfirmed model", state.ActiveModel[0].Text.MustGet())
				assert.Contains(t, state.ActiveToolCalls, "call-1")
				assert.Equal(t, "running", state.ActiveTools["call-1"])
			}
			assert.Equal(t, test.errorText, state.Transcript[len(state.Transcript)-1].Text.MustGet())
		})
	}
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
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
			Sessions:          nil,
			SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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
			Sessions:          nil,
			SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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
			Sessions:          nil,
			SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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
			Sessions:          nil,
			SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	state = service.Apply(state, presentationdomain.Event{
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("second response"),
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
