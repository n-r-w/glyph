//go:build !integration

package presentation

import "github.com/samber/mo"

// testPresentationEvent creates a private transition with optional text and stream position.
func testPresentationEvent(kind eventKind, text mo.Option[string], position mo.Option[int]) event {
	update := newEvent(kind)
	update.Text, update.Position = text, position
	return update
}

// testModelDeltaEvent creates one streamed model fragment.
func testModelDeltaEvent(position int, kind ModelContentKind, text string) event {
	update := newEvent(eventModelDelta)
	update.Position, update.ModelContentKind, update.Text = mo.Some(position), mo.Some(kind), mo.Some(text)
	return update
}

// testModelEndEvent creates a terminal model response.
func testModelEndEvent(contents ...ModelResponseContent) event {
	update := newEvent(eventModelEnd)
	update.ModelResponseContent = contents
	return update
}

// testToolOutputEvent creates one tool stream fragment.
func testToolOutputEvent(stream OutputStream, text string) event {
	update := newEvent(eventToolOutput)
	update.Stream, update.Text = mo.Some(stream), mo.Some(text)
	return update
}

// testInitializationEvent creates startup projection input without runtime dependencies.
func testInitializationEvent(startup []Line, availability Availability) event {
	update := newEvent(eventInitialization)
	update.Startup, update.Availability = startup, mo.Some(availability)
	return update
}

// testToolEndedEvent creates a terminal tool execution result.
func testToolEndedEvent(toolName, status string, failure bool) event {
	update := newEvent(eventToolEnded)
	update.ToolName, update.Status, update.Failure = mo.Some(toolName), mo.Some(status), mo.Some(failure)
	return update
}

// testFailureEvent creates a failed lifecycle update with complete diagnostic text.
func testFailureEvent(kind eventKind, errorText string) event {
	update := newEvent(kind)
	update.ErrorText, update.Failure = mo.Some(errorText), mo.Some(true)
	return update
}

// testAvailabilityEvent creates an admission-state transition.
func testAvailabilityEvent(kind eventKind, availability Availability) event {
	update := newEvent(kind)
	update.Availability = mo.Some(availability)
	return update
}

// testSessionEvent creates confirmed session metadata and restored transcript input.
func testSessionEvent(kind eventKind, info mo.Option[SessionInfo], restored []Line) event {
	update := newEvent(kind)
	update.SessionInfo, update.RestoredTranscript = info, restored
	return update
}

// projectionReasoning creates supported choices with the last choice as the default.
func projectionReasoning(choices ...ReasoningChoice) ReasoningCapabilities {
	return ReasoningCapabilities{Supported: true, Choices: choices, Default: choices[len(choices)-1]}
}
