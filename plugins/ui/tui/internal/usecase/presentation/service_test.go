package presentation

import (
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

func testPresentationEvent(
	kind presentationdomain.EventKind,
	text mo.Option[string],
	position mo.Option[int],
) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: kind, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: position,
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: text,
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

func testSessionEvent(
	kind presentationdomain.EventKind,
	info mo.Option[presentationdomain.SessionInfo],
	restored []presentationdomain.Line,
) presentationdomain.Event {
	return presentationdomain.Event{
		Kind: kind, Startup: nil, RestoredTranscript: restored, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](), SessionInfo: info, Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
