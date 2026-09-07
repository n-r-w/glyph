//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateAssignsToolCompletionStatusAndResultContentOnce verifies distinct terminal payload owners.
func TestStateAssignsToolCompletionStatusAndResultContentOnce(t *testing.T) {
	t.Parallel()

	// Arrange a running tool call and its typed result content.
	state := (projection{}).Apply(testToolEndedEvent("read", "completed", false))
	// Act by applying the terminal tool-result event.
	state = state.Apply(event{
		RestoredTranscript:   nil,
		Kind:                 eventToolResult,
		ToolName:             mo.Some("read"),
		Text:                 mo.None[string](),
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
		ExitCode:          mo.None[int](),
		Failure:           mo.Some(false),
		ToolCall:          mo.None[ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[ModelSelection](),
		SessionInfo:       mo.None[SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
	})

	// Assert the tool is finalized once with status and result content.
	assert.Equal(t, []Line{
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
	}, state.Transcript)
}

// TestStateClearsUnconfirmedModelOnlyOnPersistenceFailure verifies contextual persistence detection.
func TestStateClearsUnconfirmedModelOnlyOnPersistenceFailure(t *testing.T) {
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
			state := (projection{}).Apply(testPresentationEvent(
				eventUserSubmitted,
				mo.Some("durable user"),
				mo.None[int](),
			))
			state = state.Apply(testPresentationEvent(
				eventModelDelta,
				mo.Some("unconfirmed model"),
				mo.Some(0),
			))
			state.ActiveToolCalls = map[string]ToolCallState{
				"call-1": {
					CallID: "call-1", Name: "bash", Position: 0, Provisional: true,
					Fields: nil, Arguments: nil,
				},
			}
			state.ActiveTools = map[string]string{"call-1": "running"}

			// Act by applying a Host error after the streamed model and tool state.
			state = state.Apply(testPresentationEvent(
				eventError,
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

// TestStateRendersOneSafeErrorAcrossTerminalLifecycleEvents verifies layered failures are not duplicated.
func TestStateRendersOneSafeErrorAcrossTerminalLifecycleEvents(t *testing.T) {
	t.Parallel()

	// Arrange terminal lifecycle events that carry the same safe error.
	state := (projection{}).Apply(
		testPresentationEvent(eventModelDelta, mo.Some("partial"), mo.Some(1)),
	)
	// Act by applying every terminal event.
	for _, event := range []event{
		testFailureEvent(eventModelEnd, "Provider failed."),
		testFailureEvent(eventTurnEnded, "Provider failed."),
		{
			RestoredTranscript:   nil,
			Kind:                 eventAgentSettled,
			Failure:              mo.Some(true),
			Text:                 mo.Some("Provider failed."),
			Startup:              nil,
			Availability:         mo.None[Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[OutputStream](),
			Contents:             mo.None[[]Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			ToolCall:             mo.None[ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[ModelSelection](),
			SessionInfo:          mo.None[SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[SessionStatistics](),
			treeEvent:            mo.None[treeEvent](),
		},
		{
			RestoredTranscript:   nil,
			Kind:                 eventError,
			Text:                 mo.Some("Provider failed."),
			Startup:              nil,
			Availability:         mo.None[Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
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
		},
	} {
		state = state.Apply(event)
	}

	// Assert the transcript contains one safe error line.
	assert.Equal(t, []Line{{
		Kind:     LineError,
		Text:     mo.Some("Provider failed."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
	assert.Equal(t, mo.Some(true), state.Settled)
}

// TestStateRetainsTranscriptAcrossSettlementAndSecondTurn verifies multi-turn transcript continuity.
func TestStateRetainsTranscriptAcrossSettlementAndSecondTurn(t *testing.T) {
	t.Parallel()

	// Arrange a completed first turn and a second active turn.
	state := (projection{}).Apply(testAvailabilityEvent(
		eventInitialization, AvailabilityIdle,
	))
	// Act by settling the first turn and applying the second turn.
	state = state.Apply(testAvailabilityEvent(
		eventAvailability, AvailabilityRunning,
	))
	state = state.Apply(testModelEndEvent(ModelResponseContent{
		Kind: ModelContentText,
		Text: mo.Some("first response"),
	}))
	state = state.Apply(
		testPresentationEvent(eventAgentSettled, mo.Some("completed"), mo.None[int]()),
	)
	state = state.Apply(testAvailabilityEvent(
		eventAvailability, AvailabilityIdle,
	))
	state = state.Apply(testModelEndEvent(ModelResponseContent{
		Kind: ModelContentText,
		Text: mo.Some("second response"),
	}))

	// Assert settlement state and both turns remain projected in order.
	assert.Equal(t, mo.Some(AvailabilityIdle), state.Availability)
	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, []Line{
		{
			Kind:     LineModel,
			Text:     mo.Some("first response"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineModel,
			Text:     mo.Some("second response"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
	}, state.Transcript)
}
