//go:build !integration

package presentation

import (
	"github.com/samber/mo"
)

// testEventPayload contains the selected values for one presentation event fixture.
type testEventPayload struct {
	// Kind identifies the event behavior.
	Kind eventKind
	// Availability contains an optional availability transition.
	Availability mo.Option[Availability]
	// Position contains an optional streamed content position.
	Position mo.Option[int]
	// Text contains optional event text.
	Text mo.Option[string]
	// ModelResponseContent contains terminal model content.
	ModelResponseContent []ModelResponseContent
	// ModelSelection contains an optional selected model.
	ModelSelection mo.Option[ModelSelection]
	// SessionInfo contains optional session identity.
	SessionInfo mo.Option[SessionInfo]
}

// testEvent creates one presentation event without unrelated payloads.
func testEvent(
	payload testEventPayload,
	models ...ConfiguredModel,
) event {
	if len(models) == 0 {
		models = nil
	}
	return event{
		RestoredTranscript:   nil,
		Kind:                 payload.Kind,
		Startup:              nil,
		Availability:         payload.Availability,
		Position:             payload.Position,
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: payload.ModelResponseContent,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 payload.Text,
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		Models:               models,
		ModelSelection:       payload.ModelSelection,
		SessionInfo:          payload.SessionInfo,
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	}
}
