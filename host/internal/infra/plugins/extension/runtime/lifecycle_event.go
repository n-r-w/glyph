package runtime

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapLifecycleEvent maps one provider-neutral source event without provider reasoning context.
func mapLifecycleEvent(event lifecycle.Event) (*extensionpb.LifecycleInvocation, error) {
	if event.Settled {
		mapped := new(extensionpb.LifecycleInvocation)
		mapped.SetAgentSettled(extensionpb.AgentSettled_builder{RunId: new(event.Agent.RunID)}.Build())
		return mapped, nil
	}
	switch event.Agent.Type {
	case run.EventAgentStart, run.EventAgentEnd, run.EventTurnStart, run.EventTurnEnd:
		return mapLifecycleBoundary(event.Agent)
	case run.EventMessageStart, run.EventContentStart, run.EventTextDelta, run.EventContentEnd,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd, run.EventMessageEnd:
		return mapLifecycleMessage(event.Agent)
	case run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult:
		return mapLifecycleTool(event.Agent)
	default:
		return nil, fmt.Errorf("unsupported lifecycle event type %d", event.Agent.Type)
	}
}

// mapLifecycleBoundary maps agent and turn boundary events.
func mapLifecycleBoundary(source run.Event) (*extensionpb.LifecycleInvocation, error) {
	mapped := new(extensionpb.LifecycleInvocation)
	switch source.Type {
	case run.EventAgentStart:
		mapped.SetAgentStart(extensionpb.AgentStart_builder{RunId: new(source.RunID)}.Build())
	case run.EventAgentEnd:
		payload, err := mapLifecycleAgentEnd(source)
		if err != nil {
			return nil, err
		}
		mapped.SetAgentEnd(payload)
	case run.EventTurnStart:
		mapped.SetTurnStart(extensionpb.TurnStart_builder{RunId: new(source.RunID)}.Build())
	case run.EventTurnEnd:
		payload, err := mapLifecycleTurnEnd(source)
		if err != nil {
			return nil, err
		}
		mapped.SetTurnEnd(payload)
	case run.EventMessageStart, run.EventContentStart, run.EventTextDelta, run.EventContentEnd,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd, run.EventMessageEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult:
		return nil, fmt.Errorf("unsupported lifecycle boundary type %d", source.Type)
	default:
		return nil, fmt.Errorf("unsupported lifecycle boundary type %d", source.Type)
	}
	return mapped, nil
}

// mapLifecycleMessage maps message boundaries and content transitions.
func mapLifecycleMessage(source run.Event) (*extensionpb.LifecycleInvocation, error) {
	mapped := new(extensionpb.LifecycleInvocation)
	switch source.Type {
	case run.EventMessageStart:
		mapped.SetMessageStart(extensionpb.MessageStart_builder{RunId: new(source.RunID)}.Build())
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd:
		update, err := mapLifecycleMessageUpdate(source)
		if err != nil {
			return nil, err
		}
		mapped.SetMessageUpdate(update)
	case run.EventMessageEnd:
		payload, err := mapLifecycleMessageEnd(source)
		if err != nil {
			return nil, err
		}
		mapped.SetMessageEnd(payload)
	case run.EventAgentStart, run.EventTurnStart, run.EventToolExecutionStart,
		run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult,
		run.EventTurnEnd, run.EventAgentEnd:
		return nil, fmt.Errorf("unsupported lifecycle message type %d", source.Type)
	default:
		return nil, fmt.Errorf("unsupported lifecycle message type %d", source.Type)
	}
	return mapped, nil
}

// mapLifecycleTool maps tool execution lifecycle events.
func mapLifecycleTool(source run.Event) (*extensionpb.LifecycleInvocation, error) {
	mapped := new(extensionpb.LifecycleInvocation)
	switch source.Type {
	case run.EventToolExecutionStart:
		payload, err := mapLifecycleToolStart(source)
		if err != nil {
			return nil, err
		}
		mapped.SetToolExecutionStart(payload)
	case run.EventToolExecutionUpdate:
		payload, err := mapLifecycleToolUpdate(source)
		if err != nil {
			return nil, err
		}
		mapped.SetToolExecutionUpdate(payload)
	case run.EventToolExecutionEnd:
		payload, err := mapLifecycleToolEnd(source)
		if err != nil {
			return nil, err
		}
		mapped.SetToolExecutionEnd(payload)
	case run.EventToolResult:
		return nil, errors.New("tool-result events have no lifecycle observer group")
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventContentStart, run.EventTextDelta, run.EventContentEnd,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd,
		run.EventMessageEnd, run.EventTurnEnd, run.EventAgentEnd:
		return nil, fmt.Errorf("unsupported lifecycle tool type %d", source.Type)
	default:
		return nil, fmt.Errorf("unsupported lifecycle tool type %d", source.Type)
	}
	return mapped, nil
}

// mapLifecycleAgentEnd maps one terminal agent summary.
func mapLifecycleAgentEnd(source run.Event) (*extensionpb.AgentEnd, error) {
	payload := extensionpb.AgentEnd_builder{RunId: new(source.RunID), Outcome: nil, ErrorMessage: nil}
	if summary, present := source.Agent.Get(); present {
		outcome, err := mapLifecycleAgentOutcome(summary.Outcome)
		if err != nil {
			return nil, err
		}
		payload.Outcome = new(outcome)
		if message, available := summary.ErrorMessage.Get(); available {
			payload.ErrorMessage = new(message)
		}
	}
	return payload.Build(), nil
}

// mapLifecycleTurnEnd maps one terminal turn response.
func mapLifecycleTurnEnd(source run.Event) (*extensionpb.TurnEnd, error) {
	payload := extensionpb.TurnEnd_builder{RunId: new(source.RunID), Response: nil, ToolResults: nil}
	if summary, present := source.Turn.Get(); present {
		response, err := mapLifecycleResponse(summary.Response)
		if err != nil {
			return nil, err
		}
		payload.Response = response
		payload.ToolResults = make([]*extensionpb.ToolExecutionEnd, len(summary.ToolResults))
		for index := range summary.ToolResults {
			mapped, mapErr := mapLifecycleToolResult(source.RunID, summary.ToolResults[index])
			if mapErr != nil {
				return nil, mapErr
			}
			payload.ToolResults[index] = mapped
		}
	}
	return payload.Build(), nil
}

// mapLifecycleMessageEnd maps one finalized model response.
func mapLifecycleMessageEnd(source run.Event) (*extensionpb.MessageEnd, error) {
	payload := extensionpb.MessageEnd_builder{RunId: new(source.RunID), Response: nil}
	if response, present := source.Message.Get(); present {
		mapped, err := mapLifecycleResponse(response)
		if err != nil {
			return nil, err
		}
		payload.Response = mapped
	}
	return payload.Build(), nil
}

// mapLifecycleToolStart maps one tool execution identity.
func mapLifecycleToolStart(source run.Event) (*extensionpb.ToolExecutionStart, error) {
	call, present := source.ToolCall.Get()
	if !present {
		return nil, errors.New("tool execution start is missing its tool call")
	}
	return extensionpb.ToolExecutionStart_builder{
		RunId: new(source.RunID), CallId: new(call.ID), ToolName: new(call.Name),
	}.Build(), nil
}

// mapLifecycleToolUpdate maps one tool progress fragment.
func mapLifecycleToolUpdate(source run.Event) (*extensionpb.ToolExecutionUpdate, error) {
	call, callPresent := source.ToolCall.Get()
	if !callPresent {
		return nil, errors.New("tool execution update is missing its tool call")
	}
	progress, present := source.Progress.Get()
	if !present {
		return nil, errors.New("tool execution update is missing progress")
	}
	channel, err := mapLifecycleProgressChannel(progress.Channel)
	if err != nil {
		return nil, err
	}
	return extensionpb.ToolExecutionUpdate_builder{
		RunId: new(source.RunID), CallId: new(call.ID), ToolName: new(call.Name),
		Channel: new(channel), Content: new(progress.Content),
	}.Build(), nil
}

// mapLifecycleToolEnd maps one terminal tool result.
func mapLifecycleToolEnd(source run.Event) (*extensionpb.ToolExecutionEnd, error) {
	result, present := source.ToolResult.Get()
	if !present {
		return nil, errors.New("tool execution end is missing its result")
	}
	return mapLifecycleToolResult(source.RunID, result)
}

// mapLifecycleToolResult maps one ordered terminal tool result.
func mapLifecycleToolResult(runID string, result agent.ToolResult) (*extensionpb.ToolExecutionEnd, error) {
	contents, err := mapLifecycleToolContents(result.Contents)
	if err != nil {
		return nil, err
	}
	return extensionpb.ToolExecutionEnd_builder{
		RunId: new(runID), CallId: new(result.CallID), ToolName: new(result.ToolName),
		Contents: contents, IsError: new(result.IsError),
	}.Build(), nil
}

// mapLifecycleMessageUpdate maps content and tool-call transitions in source order.
func mapLifecycleMessageUpdate(event run.Event) (*extensionpb.MessageUpdate, error) {
	kind, kindErr := lifecycleMessageUpdateKind(event.Type)
	if kindErr != nil {
		return nil, kindErr
	}
	builder := extensionpb.MessageUpdate_builder{
		RunId:    new(event.RunID),
		Kind:     new(kind),
		Position: nil,
		Content:  nil,
		ToolCall: nil,
	}
	if position, present := event.Position.Get(); present {
		builder.Position = new(int64(position))
	}
	if content, present := event.Content.Get(); present {
		mapped, visible, err := mapLifecycleContent(content)
		if err != nil {
			return nil, err
		}
		if visible {
			builder.Content = mapped
		}
	}
	if call, present := event.ToolCall.Get(); present {
		arguments, err := json.Marshal(call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("encode lifecycle tool call arguments: %w", err)
		}
		builder.ToolCall = extensionpb.LifecycleToolCall_builder{
			Id: new(call.ID), Name: new(call.Name), Position: builder.Position,
			Provisional: new(false), Fields: nil, ArgumentsJson: arguments,
		}.Build()
	}
	if preview, present := event.Preview.Get(); present && builder.ToolCall == nil {
		fields, err := mapLifecyclePreviewFields(preview.Fields)
		if err != nil {
			return nil, err
		}
		builder.ToolCall = extensionpb.LifecycleToolCall_builder{
			Id: new(preview.CallID), Name: new(preview.Name), Position: new(int64(preview.Position)),
			Provisional: new(preview.Provisional), Fields: fields, ArgumentsJson: nil,
		}.Build()
	}
	return builder.Build(), nil
}

// mapLifecyclePreviewFields maps complete JSON values and exact scalar prefixes.
func mapLifecyclePreviewFields(fields []model.ToolCallPreviewField) ([]*extensionpb.LifecycleToolCallField, error) {
	mapped := make([]*extensionpb.LifecycleToolCallField, len(fields))
	for index := range fields {
		field := new(extensionpb.LifecycleToolCallField)
		switch fields[index].Kind {
		case model.ToolCallPreviewFieldComplete:
			value, present := fields[index].Value.Get()
			if !present {
				return nil, errors.New("complete lifecycle tool-call field value is missing")
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("encode lifecycle tool-call field %q: %w", fields[index].Name, err)
			}
			field.SetValueJson(encoded)
		case model.ToolCallPreviewFieldPrefix:
			prefix, present := fields[index].Prefix.Get()
			if !present {
				return nil, errors.New("lifecycle tool-call field prefix is missing")
			}
			field.SetPrefix(prefix)
		default:
			return nil, fmt.Errorf("unknown lifecycle tool-call field kind %d", fields[index].Kind)
		}
		field.SetName(fields[index].Name)
		mapped[index] = field
	}
	return mapped, nil
}

// lifecycleMessageUpdateKind maps one source transition to its public message-update kind.
func lifecycleMessageUpdateKind(eventType run.EventType) (extensionpb.MessageUpdateKind, error) {
	switch eventType {
	case run.EventContentStart:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_CONTENT_START, nil
	case run.EventTextDelta:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_TEXT_DELTA, nil
	case run.EventContentEnd:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_CONTENT_END, nil
	case run.EventToolCallStart:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_TOOL_CALL_START, nil
	case run.EventToolCallDelta:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_TOOL_CALL_UPDATE, nil
	case run.EventToolCallEnd:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_TOOL_CALL_END, nil
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart, run.EventMessageEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd,
		run.EventToolResult, run.EventTurnEnd, run.EventAgentEnd:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_UNSPECIFIED,
			fmt.Errorf("unsupported message update type %d", eventType)
	default:
		return extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_UNSPECIFIED,
			fmt.Errorf("unknown message update type %d", eventType)
	}
}
