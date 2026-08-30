package tui

import (
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// testEventPayload contains the selected values for one presentation event fixture.
type testEventPayload struct {
	// Kind identifies the event behavior.
	Kind presentationdomain.EventKind
	// Availability contains an optional availability transition.
	Availability mo.Option[presentationdomain.Availability]
	// Position contains an optional streamed content position.
	Position mo.Option[int]
	// Text contains optional event text.
	Text mo.Option[string]
	// ModelResponseContent contains terminal model content.
	ModelResponseContent []presentationdomain.ModelResponseContent
	// ModelSelection contains an optional selected model.
	ModelSelection mo.Option[presentationdomain.ModelSelection]
	// SessionInfo contains optional session identity.
	SessionInfo mo.Option[presentationdomain.SessionInfo]
}

// testEvent creates one presentation event without unrelated payloads.
func testEvent(
	payload testEventPayload,
	models ...presentationdomain.ConfiguredModel,
) presentationdomain.Event {
	if len(models) == 0 {
		models = nil
	}
	return presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 payload.Kind,
		Startup:              nil,
		Extensions:           nil,
		Availability:         payload.Availability,
		Position:             payload.Position,
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: payload.ModelResponseContent,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 payload.Text,
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               models,
		ModelSelection:       payload.ModelSelection,
		SessionInfo:          payload.SessionInfo,
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}
}
