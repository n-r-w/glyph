package programmatic

import (
	"errors"
	"fmt"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// mapEvent maps internal operation progress to a Programmatic agent event.
func mapEvent(event AgentEvent) (*programmaticv1.AgentEvent, error) {
	eventType, err := mapAgentEventType(event.Type)
	if err != nil {
		return nil, err
	}
	wire := new(programmaticv1.AgentEvent)
	wire.SetType(eventType)
	wire.SetRunId(event.RunID)

	switch event.Type {
	case AgentEventAgentStart, AgentEventTurnStart, AgentEventMessageStart:
	case AgentEventModelContentStart, AgentEventModelTextDelta, AgentEventModelContentEnd, AgentEventMessageEnd:
		err = mapModelEvent(event, wire)
	case AgentEventToolCallStart, AgentEventToolCallDelta, AgentEventToolCallEnd:
		err = mapToolCallEvent(event, wire)
	case AgentEventToolExecutionStart, AgentEventToolExecutionUpdate, AgentEventToolExecutionEnd, AgentEventToolResult:
		err = mapToolExecutionEvent(event, wire)
	case AgentEventTurnEnd, AgentEventAgentEnd:
		err = mapTerminalEvent(event, wire)
	case AgentEventUnspecified:
		return nil, errors.New("map agent event: unspecified event type")
	default:
		return nil, fmt.Errorf("map agent event: unknown event type %d", event.Type)
	}
	if err != nil {
		return nil, err
	}
	return wire, nil
}

// mapModelEvent maps model progress and terminal model data.
func mapModelEvent(event AgentEvent, wire *programmaticv1.AgentEvent) error {
	switch event.Type {
	case AgentEventModelContentStart, AgentEventModelTextDelta, AgentEventModelContentEnd:
		contentValue, present := event.ModelContent.Get()
		if !present {
			return fmt.Errorf("map agent event type %d: model content is missing", event.Type)
		}
		content, err := mapModelContent(contentValue, event.Type == AgentEventModelTextDelta)
		if err != nil {
			return err
		}
		wire.SetModelContent(content)
	case AgentEventMessageEnd:
		responseValue, present := event.ModelResponse.Get()
		if !present {
			return errors.New("map message end event: model response is missing")
		}
		response, err := mapModelResponse(responseValue)
		if err != nil {
			return err
		}
		wire.SetModelResponse(response)
	case AgentEventUnspecified, AgentEventAgentStart, AgentEventTurnStart, AgentEventMessageStart,
		AgentEventToolCallStart, AgentEventToolCallDelta, AgentEventToolCallEnd,
		AgentEventToolExecutionStart, AgentEventToolExecutionUpdate, AgentEventToolExecutionEnd,
		AgentEventToolResult, AgentEventTurnEnd, AgentEventAgentEnd:
		return fmt.Errorf("map model event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map model event: unsupported event type %d", event.Type)
	}
	return nil
}

// mapToolCallEvent maps incremental and final tool-call data.
func mapToolCallEvent(event AgentEvent, wire *programmaticv1.AgentEvent) error {
	switch event.Type {
	case AgentEventToolCallStart, AgentEventToolCallDelta:
		previewValue, present := event.ToolCallPreview.Get()
		if !present {
			return fmt.Errorf("map agent event type %d: tool call preview is missing", event.Type)
		}
		preview, err := mapToolCallPreview(previewValue)
		if err != nil {
			return err
		}
		wire.SetToolCallPreview(preview)
	case AgentEventToolCallEnd:
		callValue, present := event.FinalToolCall.Get()
		if !present {
			return errors.New("map tool call end event: final tool call is missing")
		}
		call, err := mapFinalToolCall(callValue)
		if err != nil {
			return err
		}
		wire.SetFinalToolCall(call)
	case AgentEventUnspecified, AgentEventAgentStart, AgentEventTurnStart, AgentEventMessageStart,
		AgentEventModelContentStart, AgentEventModelTextDelta, AgentEventModelContentEnd, AgentEventMessageEnd,
		AgentEventToolExecutionStart, AgentEventToolExecutionUpdate, AgentEventToolExecutionEnd,
		AgentEventToolResult, AgentEventTurnEnd, AgentEventAgentEnd:
		return fmt.Errorf("map tool call event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map tool call event: unsupported event type %d", event.Type)
	}
	return nil
}

// mapToolExecutionEvent maps tool execution progress and results.
func mapToolExecutionEvent(event AgentEvent, wire *programmaticv1.AgentEvent) error {
	switch event.Type {
	case AgentEventToolExecutionStart:
		executionValue, present := event.ToolExecution.Get()
		if !present {
			return errors.New("map tool execution start event: tool execution is missing")
		}
		execution := new(programmaticv1.ToolExecution)
		execution.SetCallId(executionValue.CallID)
		execution.SetToolName(executionValue.ToolName)
		wire.SetToolExecution(execution)
	case AgentEventToolExecutionUpdate:
		progressValue, present := event.ToolProgress.Get()
		if !present {
			return errors.New("map tool execution update event: tool progress is missing")
		}
		progress, err := mapToolProgress(progressValue)
		if err != nil {
			return err
		}
		wire.SetToolProgress(progress)
	case AgentEventToolExecutionEnd, AgentEventToolResult:
		resultValue, present := event.ToolResult.Get()
		if !present {
			return fmt.Errorf("map agent event type %d: tool result is missing", event.Type)
		}
		result, err := mapToolResult(resultValue)
		if err != nil {
			return err
		}
		wire.SetToolResult(result)
	case AgentEventUnspecified, AgentEventAgentStart, AgentEventTurnStart, AgentEventMessageStart,
		AgentEventModelContentStart, AgentEventModelTextDelta, AgentEventModelContentEnd,
		AgentEventToolCallStart, AgentEventToolCallDelta, AgentEventToolCallEnd, AgentEventMessageEnd,
		AgentEventTurnEnd, AgentEventAgentEnd:
		return fmt.Errorf("map tool execution event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map tool execution event: unsupported event type %d", event.Type)
	}
	return nil
}

// mapTerminalEvent maps turn and agent terminal progress.
func mapTerminalEvent(event AgentEvent, wire *programmaticv1.AgentEvent) error {
	switch event.Type {
	case AgentEventTurnEnd:
		turnValue, present := event.Turn.Get()
		if !present {
			return errors.New("map turn end event: turn summary is missing")
		}
		turn, err := mapTurnSummary(turnValue)
		if err != nil {
			return err
		}
		wire.SetTurn(turn)
	case AgentEventAgentEnd:
		agentValue, present := event.Agent.Get()
		if !present {
			return errors.New("map agent end event: agent summary is missing")
		}
		agent, err := mapAgentSummary(agentValue)
		if err != nil {
			return err
		}
		wire.SetAgent(agent)
	case AgentEventUnspecified, AgentEventAgentStart, AgentEventTurnStart, AgentEventMessageStart,
		AgentEventModelContentStart, AgentEventModelTextDelta, AgentEventModelContentEnd,
		AgentEventToolCallStart, AgentEventToolCallDelta, AgentEventToolCallEnd, AgentEventMessageEnd,
		AgentEventToolExecutionStart, AgentEventToolExecutionUpdate, AgentEventToolExecutionEnd,
		AgentEventToolResult:
		return fmt.Errorf("map terminal event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map terminal event: unsupported event type %d", event.Type)
	}
	return nil
}
