//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateProjectsAuthorizationInformationAndSafeErrors verifies standalone Host frames remain visible.
func TestStateProjectsAuthorizationInformationAndSafeErrors(t *testing.T) {
	t.Parallel()

	// Arrange authorization, information, and safe-error lifecycle events.
	state := (projection{}).Apply(
		testPresentationEvent(
			eventAuthorization, mo.Some("https://example.test/oauth"), mo.None[int](),
		),
	)
	// Act by applying the information and safe-error events.
	state = state.Apply(
		testPresentationEvent(
			eventInformation, mo.Some("Open the authorization URL."), mo.None[int](),
		),
	)
	state = state.Apply(
		testPresentationEvent(eventError, mo.Some("Authentication failed."), mo.None[int]()),
	)
	state = state.Apply(testAvailabilityEvent(
		eventAvailability, AvailabilityAuthenticationFailed,
	))

	// Assert authorization state and safe transcript lines are projected.
	assert.Equal(t, mo.Some("https://example.test/oauth"), state.AuthorizationURL)
	assert.Equal(t, mo.Some(AvailabilityAuthenticationFailed), state.Availability)
	assert.Equal(t, []Line{
		{
			Kind:     LineInformation,
			Text:     mo.Some("Open the authorization URL."),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineError,
			Text:     mo.Some("Authentication failed."),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
	}, state.Transcript)
}

// TestStatePreservesAbsentStateAndCopiesOptionalJSON verifies None state and mutable Some payload isolation.
func TestStatePreservesAbsentStateAndCopiesOptionalJSON(t *testing.T) {
	t.Parallel()

	// Arrange absent optional state and nested caller-owned JSON values.
	value := map[string]any{
		"nested": []any{[]byte{1, 2, 3}},
	}
	state := (projection{}).Apply(event{
		FailureCode:          "",
		RestoredTranscript:   nil,
		Kind:                 eventToolCallPreview,
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
		ToolCall: mo.Some(ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    0,
			Provisional: true,
			Fields: []ToolCallField{{
				Name:   "value",
				Value:  mo.Some[any](value),
				Prefix: mo.None[string](),
			}},
			Arguments: nil,
		}),
		Models:            nil,
		ModelSelection:    mo.None[ModelSelection](),
		SessionInfo:       mo.None[SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
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
	state = state.Apply(event{
		FailureCode:          "",
		RestoredTranscript:   nil,
		Kind:                 eventModelDelta,
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.Some(0),
		ModelContentKind:     mo.Some(ModelContentText),
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
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	})
	// Assert absence is preserved and nested JSON is independently owned.
	assert.Equal(t, mo.Some(ModelContentText), state.ActiveModel[0].Kind)
	assert.True(t, state.ActiveModel[0].Text.IsNone())

	state = state.Apply(testPresentationEvent(eventTurnStarted, mo.None[string](), mo.None[int]()))
	assert.Equal(t, mo.Some(false), state.Settled)
}

// TestStateIgnoresMissingSelectedPayload verifies malformed events do not project zero payloads.
func TestStateIgnoresMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	// Arrange an information event without any selected payload.
	event := testPresentationEvent(eventInformation, mo.None[string](), mo.None[int]())

	// Act by applying the incomplete information event.
	state := (projection{}).Apply(event)

	// Assert the incomplete event adds no transcript content.
	assert.Empty(t, state.Transcript)
}
