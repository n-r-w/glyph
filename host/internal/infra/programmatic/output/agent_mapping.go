package output

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// mapAgentEvent maps one Agent Core event to Programmatic operation progress.
func mapAgentEvent(event agent.Event) (controller.AgentEvent, error) {
	mapped := controller.AgentEvent{
		OperationID:     "",
		Type:            mapAgentEventType(event.Type),
		RunID:           event.RunID,
		ModelContent:    mo.None[controller.ModelContent](),
		ToolCallPreview: mo.None[controller.ToolCallPreview](),
		FinalToolCall:   mo.None[controller.FinalToolCall](),
		ToolExecution:   mo.None[controller.ToolExecution](),
		ToolProgress:    mo.None[controller.ToolProgress](),
		ToolResult:      mo.None[controller.ToolResult](),
		ModelResponse:   mo.None[controller.ModelResponse](),
		Turn:            mo.None[controller.TurnSummary](),
		Agent:           mo.None[controller.AgentSummary](),
	}
	var err error
	switch event.Type {
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart:
	case agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd, agent.EventMessageEnd:
		err = mapProgrammaticModelEvent(event, &mapped)
	case agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd,
		agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd, agent.EventToolResult:
		err = mapProgrammaticToolEvent(event, &mapped)
	case agent.EventTurnEnd, agent.EventAgentEnd:
		err = mapProgrammaticTerminalEvent(event, &mapped)
	}
	if err != nil {
		return controller.AgentEvent{}, err
	}
	return mapped, nil
}

// mapProgrammaticModelEvent maps selected model payloads to Programmatic Control.
func mapProgrammaticModelEvent(event agent.Event, mapped *controller.AgentEvent) error {
	switch event.Type {
	case agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd:
		content, hasContent := event.Content.Get()
		position, hasPosition := event.Position.Get()
		if !hasContent || !hasPosition {
			return fmt.Errorf("event type %d requires content and position", event.Type)
		}
		text := mo.None[string]()
		if event.Type == agent.EventTextDelta {
			value, present := content.Text.Get()
			if !present {
				return errors.New("text delta event requires text")
			}
			text = mo.Some(value)
		}
		mapped.ModelContent = mo.Some(controller.ModelContent{
			Kind: controller.MapModelContentKind(content.Kind), Position: position, Text: text,
		})
	case agent.EventMessageEnd:
		message, present := event.Message.Get()
		if !present {
			return errors.New("message end event requires model response")
		}
		response, err := controller.MapModelResponseProjection(message)
		if err != nil {
			return err
		}
		mapped.ModelResponse = mo.Some(response)
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart,
		agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd,
		agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd,
		agent.EventToolResult, agent.EventTurnEnd, agent.EventAgentEnd:
		return fmt.Errorf("unsupported Programmatic Control model event type %d", event.Type)
	}
	return nil
}

// mapProgrammaticToolEvent maps selected tool payloads to Programmatic Control.
func mapProgrammaticToolEvent(event agent.Event, mapped *controller.AgentEvent) error {
	switch event.Type {
	case agent.EventToolCallStart, agent.EventToolCallDelta:
		preview, present := event.Preview.Get()
		if !present {
			return fmt.Errorf("event type %d requires tool call preview", event.Type)
		}
		mapped.ToolCallPreview = mo.Some(controller.MapToolCallPreview(preview))
	case agent.EventToolCallEnd:
		call, hasCall := event.ToolCall.Get()
		position, hasPosition := event.Position.Get()
		if !hasCall || !hasPosition {
			return errors.New("tool call end event requires tool call and position")
		}
		mapped.FinalToolCall = mo.Some(controller.FinalToolCall{
			CallID: call.ID, Name: call.Name, Position: position, Arguments: call.Clone().Arguments,
		})
	case agent.EventToolExecutionStart:
		call, present := event.ToolCall.Get()
		if !present {
			return errors.New("tool execution start event requires tool call")
		}
		mapped.ToolExecution = mo.Some(controller.ToolExecution{CallID: call.ID, ToolName: call.Name})
	case agent.EventToolExecutionUpdate:
		progress, present := event.Progress.Get()
		if !present {
			return errors.New("tool execution update event requires progress")
		}
		mapped.ToolProgress = mo.Some(controller.ToolProgress{
			Channel: controller.MapProgressChannel(progress.Channel), Content: progress.Content,
		})
	case agent.EventToolExecutionEnd, agent.EventToolResult:
		result, present := event.ToolResult.Get()
		if !present {
			return fmt.Errorf("event type %d requires tool result", event.Type)
		}
		mapped.ToolResult = mo.Some(controller.MapToolResult(result))
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart,
		agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd, agent.EventMessageEnd,
		agent.EventTurnEnd, agent.EventAgentEnd:
		return fmt.Errorf("unsupported Programmatic Control tool event type %d", event.Type)
	}
	return nil
}

// mapProgrammaticTerminalEvent maps selected terminal summaries to Programmatic Control.
func mapProgrammaticTerminalEvent(event agent.Event, mapped *controller.AgentEvent) error {
	switch event.Type {
	case agent.EventTurnEnd:
		turn, present := event.Turn.Get()
		if !present {
			return errors.New("turn end event requires turn summary")
		}
		toolResults := lo.Map(turn.ToolResults, func(result agent.ToolResult, _ int) controller.ToolResult {
			return controller.MapToolResult(result)
		})
		response, err := controller.MapModelResponseProjection(turn.Response)
		if err != nil {
			return err
		}
		mapped.Turn = mo.Some(controller.TurnSummary{Response: response, ToolResults: toolResults})
	case agent.EventAgentEnd:
		summary, present := event.Agent.Get()
		if !present {
			return errors.New("agent end event requires agent summary")
		}
		mapped.Agent = mo.Some(controller.AgentSummary{
			Outcome: controller.MapRunOutcome(summary.Outcome), ErrorMessage: summary.ErrorMessage,
		})
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart,
		agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd,
		agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd, agent.EventMessageEnd,
		agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd, agent.EventToolResult:
		return fmt.Errorf("unsupported Programmatic Control terminal event type %d", event.Type)
	}
	return nil
}

// mapAgentEventType maps nonterminal Agent Core event types.
func mapAgentEventType(eventType agent.EventType) controller.AgentEventType {
	switch eventType {
	case agent.EventAgentStart:
		return controller.AgentEventAgentStart
	case agent.EventTurnStart:
		return controller.AgentEventTurnStart
	case agent.EventMessageStart:
		return controller.AgentEventMessageStart
	case agent.EventContentStart:
		return controller.AgentEventModelContentStart
	case agent.EventTextDelta:
		return controller.AgentEventModelTextDelta
	case agent.EventContentEnd:
		return controller.AgentEventModelContentEnd
	case agent.EventToolCallStart:
		return controller.AgentEventToolCallStart
	case agent.EventToolCallDelta:
		return controller.AgentEventToolCallDelta
	case agent.EventToolCallEnd,
		agent.EventMessageEnd,
		agent.EventToolExecutionStart,
		agent.EventToolExecutionUpdate,
		agent.EventToolExecutionEnd,
		agent.EventToolResult,
		agent.EventTurnEnd,
		agent.EventAgentEnd:
		return mapTerminalAgentEventType(eventType)
	default:
		return controller.AgentEventUnspecified
	}
}

// mapTerminalAgentEventType maps terminal Agent Core event types.
func mapTerminalAgentEventType(eventType agent.EventType) controller.AgentEventType {
	switch eventType {
	case agent.EventToolCallEnd:
		return controller.AgentEventToolCallEnd
	case agent.EventMessageEnd:
		return controller.AgentEventMessageEnd
	case agent.EventToolExecutionStart:
		return controller.AgentEventToolExecutionStart
	case agent.EventToolExecutionUpdate:
		return controller.AgentEventToolExecutionUpdate
	case agent.EventToolExecutionEnd:
		return controller.AgentEventToolExecutionEnd
	case agent.EventToolResult:
		return controller.AgentEventToolResult
	case agent.EventTurnEnd:
		return controller.AgentEventTurnEnd
	case agent.EventAgentEnd:
		return controller.AgentEventAgentEnd
	case agent.EventAgentStart,
		agent.EventTurnStart,
		agent.EventMessageStart,
		agent.EventContentStart,
		agent.EventTextDelta,
		agent.EventContentEnd,
		agent.EventToolCallStart,
		agent.EventToolCallDelta:
		return controller.AgentEventUnspecified
	}
	return controller.AgentEventUnspecified
}
