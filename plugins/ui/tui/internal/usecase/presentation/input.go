package presentation

import (
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/samber/mo/option"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// initializationEvent constructs the initial application projection from validated startup facts.
func initializationEvent(input plugininput.Initialization) event {
	result := newEvent(eventInitialization)
	result.Availability = mo.Some(Availability(input.Availability))
	result.Startup = lo.Map(
		input.Startup,
		func(line plugininput.Transcript, _ int) Line { return decodeTranscript(line) },
	)
	result.Models = lo.Map(input.Models, func(model plugininput.ConfiguredModel, _ int) ConfiguredModel {
		return decodeConfiguredModel(model)
	})
	result.ModelSelection = mo.Some(decodeModelSelection(input.Selection))
	result.SessionInfo = mo.Some(decodeSessionInfo(input.Session))
	return result
}

// applyInput selects the application transition for one validated Host payload.
func (service *Service) applyInput(input mo.Option[plugininput.Payload]) error {
	payload, present := input.Get()
	if !present {
		return nil
	}
	switch payload.Kind {
	case plugininput.PayloadAgent:
		service.model = service.model.applyEvent(agentEvent(payload.Agent))
	case plugininput.PayloadText:
		kind := eventInformation
		switch payload.Text.Kind {
		case plugininput.TextAuthorization:
			kind = eventAuthorization
		case plugininput.TextError:
			kind = eventError
		case plugininput.TextInformation:
		}
		update := textEvent(kind, payload.Text.Text)
		update.FailureCode = payload.Text.FailureCode
		service.model = service.model.applyEvent(update)
	case plugininput.PayloadAvailability:
		update := newEvent(eventAvailability)
		update.Availability = mo.Some(Availability(payload.Availability))
		service.model = service.model.applyEvent(update)
	case plugininput.PayloadSelection:
		update := newEvent(eventModelSelectionChanged)
		update.ModelSelection = mo.Some(decodeModelSelection(payload.Selection))
		service.model = service.model.applyEvent(update)
	case plugininput.PayloadSettled:
		service.model = service.model.applyEvent(newEvent(eventAgentSettled))
	case plugininput.PayloadSession:
		service.model = service.model.applyEvent(sessionInputEvent(payload.Session))
	case plugininput.PayloadTree:
		service.model = service.model.applyEvent(treeInputEvent(payload.Tree))
	case plugininput.PayloadUnspecified:
		return fmt.Errorf("unknown TUI input payload kind %d", payload.Kind)
	default:
		return fmt.Errorf("unknown TUI input payload kind %d", payload.Kind)
	}
	return nil
}

// agentEvent applies model/tool input without importing transport state into the reducer.
func agentEvent(input plugininput.AgentUpdate) event {
	kind := eventUnspecified
	switch input.Kind {
	case plugininput.AgentTurnStarted:
		kind = eventTurnStarted
	case plugininput.AgentModelDelta:
		kind = eventModelDelta
	case plugininput.AgentModelEnd:
		kind = eventModelEnd
	case plugininput.AgentToolCallPreview:
		kind = eventToolCallPreview
	case plugininput.AgentToolCallFinal:
		kind = eventToolCallFinal
	case plugininput.AgentToolStarted:
		kind = eventToolStarted
	case plugininput.AgentToolProgress:
		kind = eventToolProgress
	case plugininput.AgentToolOutput:
		kind = eventToolOutput
	case plugininput.AgentToolEnded:
		kind = eventToolEnded
	case plugininput.AgentToolResult:
		kind = eventToolResult
	case plugininput.AgentTurnEnded:
		kind = eventTurnEnded
	case plugininput.AgentUnspecified:
	}
	update := newEvent(kind)
	update.Position = input.Position
	update.ModelContentKind = option.Map(func(kind plugininput.ModelContentKind) ModelContentKind {
		return ModelContentKind(kind)
	})(input.ModelContentKind)
	update.ModelResponseContent = lo.Map(input.ModelResponseContent,
		func(content plugininput.ModelResponseContent, _ int) ModelResponseContent {
			return decodeModelResponseContent(content)
		})
	update.ToolCallID, update.ToolName, update.Status = input.ToolCallID, input.ToolName, input.Status
	update.Stream = option.Map(
		func(stream plugininput.OutputStream) OutputStream { return OutputStream(stream) },
	)(
		input.Stream,
	)
	update.Text, update.ErrorText = input.Text, input.ErrorText
	update.ExitCode, update.Failure = input.ExitCode, input.Failure
	update.Contents = option.Map(func(contents []plugininput.Content) []Content {
		return lo.Map(contents, func(content plugininput.Content, _ int) Content { return decodeContent(content) })
	})(input.Contents)
	update.ToolCall = option.Map(decodeToolCallState)(input.ToolCall)
	return update
}

// sessionInputEvent selects confirmed replacement or query behavior without exposing private state.
func sessionInputEvent(input plugininput.SessionUpdate) event {
	kind := eventSessionList
	switch input.Kind {
	case plugininput.SessionChanged:
		kind = eventSessionChanged
	case plugininput.SessionInformation:
		kind = eventSessionInformation
	case plugininput.SessionListed:
	}
	update := newEvent(kind)
	update.SessionInfo = option.Map(decodeSessionInfo)(input.Info)
	update.Sessions = lo.Map(input.Sessions, func(summary plugininput.SessionSummary, _ int) SessionSummary {
		return decodeSessionSummary(summary)
	})
	update.RestoredTranscript = lo.Map(
		input.Transcript,
		func(line plugininput.Transcript, _ int) Line { return decodeTranscript(line) },
	)
	update.SessionStatistics = option.Map(decodeSessionStatistics)(input.Statistics)
	return update
}

// treeInputEvent retains separate committed-progress and terminal-interaction transitions.
func treeInputEvent(input plugininput.TreeUpdate) event {
	kind := eventSessionTree
	switch input.Kind {
	case plugininput.TreeNavigationProgress:
		kind = eventSessionTreeNavigationProgress
	case plugininput.TreeNavigationCompleted:
		kind = eventSessionTreeNavigation
	case plugininput.TreeForked:
		kind = eventSessionForked
	case plugininput.TreeCloned:
		kind = eventSessionCloned
	case plugininput.TreeLabelSet:
		kind = eventEntryLabelSet
	case plugininput.TreeEntryAdded:
		kind = eventSessionEntryAdded
	case plugininput.TreeSnapshot:
	}
	update := newEvent(kind)
	update.treeEvent = mo.Some(treeEvent{
		Tree:             option.Map(decodeSessionTree)(input.Tree),
		NavigationStatus: TreeNavigationStatus(input.NavigationStatus),
		SessionInfo:      option.Map(decodeSessionInfo)(input.SessionInfo),
		RestoredTranscript: lo.Map(
			input.Transcript,
			func(line plugininput.Transcript, _ int) Line { return decodeTranscript(line) },
		),
		NextInput: input.NextInput,
		Issues: lo.Map(
			input.Issues,
			func(issue plugininput.OperationIssue, _ int) OperationIssue { return decodeOperationIssue(issue) },
		),
		FailureMessage: mo.None[string](),
		AddedEntry:     option.Map(decodeTreeEntry)(input.AddedEntry),
	})
	return update
}

// textEvent creates a private text transition selected by application policy.
func textEvent(kind eventKind, text string) event {
	update := newEvent(kind)
	update.Text = mo.Some(text)
	return update
}

// newEvent creates a private transition with no active payload fields.
func newEvent(kind eventKind) event {
	return event{
		FailureCode:          "",
		Kind:                 kind,
		RestoredTranscript:   nil,
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
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	}
}
