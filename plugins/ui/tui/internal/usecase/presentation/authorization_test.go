package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestServiceProjectsAuthorizationInformationAndSafeErrors verifies standalone Host frames remain visible.
func TestServiceProjectsAuthorizationInformationAndSafeErrors(t *testing.T) {
	t.Parallel()

	// Arrange authorization, information, and safe-error lifecycle events.
	service := New()
	state := service.Apply(presentationdomain.State{}, testPresentationEvent(presentationdomain.EventAuthorization, mo.Some("https://example.test/oauth"), mo.None[int]()))
	// Act by applying the information and safe-error events.
	state = service.Apply(state, testPresentationEvent(presentationdomain.EventInformation, mo.Some("Open the authorization URL."), mo.None[int]()))
	state = service.Apply(state, testPresentationEvent(presentationdomain.EventError, mo.Some("Authentication failed."), mo.None[int]()))
	state = service.Apply(state, testAvailabilityEvent(
		presentationdomain.EventAvailability, presentationdomain.AvailabilityAuthenticationFailed,
	))

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
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	// Assert absence is preserved and nested JSON is independently owned.
	assert.Equal(t, mo.Some(presentationdomain.ModelContentText), state.ActiveModel[0].Kind)
	assert.True(t, state.ActiveModel[0].Text.IsNone())

	state = New().Apply(state, testPresentationEvent(presentationdomain.EventTurnStarted, mo.None[string](), mo.None[int]()))
	assert.Equal(t, mo.Some(false), state.Settled)
}

// TestServiceIgnoresMissingSelectedPayload verifies malformed events do not project zero payloads.
func TestServiceIgnoresMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	// Arrange an information event without any selected payload.
	event := testPresentationEvent(presentationdomain.EventInformation, mo.None[string](), mo.None[int]())

	// Act by applying the incomplete information event.
	state := New().Apply(presentationdomain.State{}, event)

	// Assert the incomplete event adds no transcript content.
	assert.Empty(t, state.Transcript)
}
