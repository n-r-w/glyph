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

// testModelDeltaEvent creates one typed streamed model fragment.
func testModelDeltaEvent(
	position int,
	kind presentationdomain.ModelContentKind,
	text string,
) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: presentationdomain.EventModelDelta, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.Some(position),
		ModelContentKind: mo.Some(kind), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.Some(text),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

// testModelEndEvent creates one terminal model response event.
func testModelEndEvent(contents ...presentationdomain.ModelResponseContent) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: presentationdomain.EventModelEnd, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: contents,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

// testToolOutputEvent creates one tool output event.
func testToolOutputEvent(stream presentationdomain.OutputStream, text string) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: presentationdomain.EventToolOutput, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.Some(stream), Text: mo.Some(text), Contents: mo.None[[]presentationdomain.Content](),
		ErrorText: mo.None[string](), ExitCode: mo.None[int](), Failure: mo.None[bool](),
		ToolCall: mo.None[presentationdomain.ToolCallState](), Models: nil,
		ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo:    mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

// testInitializationEvent creates one initialization event with startup presentation data.
func testInitializationEvent(
	startup []presentationdomain.Line,
	availability presentationdomain.Availability,
	extensions []presentationdomain.Extension,
) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: presentationdomain.EventInitialization, Startup: startup, Extensions: extensions,
		Availability: mo.Some(availability), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

// testToolEndedEvent creates one terminal tool execution event.
func testToolEndedEvent(toolName, status string, failure bool) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: presentationdomain.EventToolEnded, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.Some(toolName), Status: mo.Some(status),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.Some(failure), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

// testFailureEvent creates one failed lifecycle event with its safe error text.
func testFailureEvent(kind presentationdomain.EventKind, errorText string) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: kind, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.Some(errorText),
		ExitCode: mo.None[int](), Failure: mo.Some(true), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	}
}

// testAvailabilityEvent creates one availability transition event.
func testAvailabilityEvent(
	kind presentationdomain.EventKind,
	availability presentationdomain.Availability,
) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: kind, Startup: nil, Extensions: nil,
		Availability: mo.Some(availability), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
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
