package programmatic

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/types/known/structpb"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

func mapResponse(response Response) (*programmaticv1.OpenResponse, error) {
	wire := new(programmaticv1.CommandResponse)
	switch response.Kind {
	case ResponseUserRequestAccepted:
		wire.SetUserRequestAccepted(new(programmaticv1.UserRequestAccepted))
	case ResponseAbortCompleted:
		wire.SetAbortCompleted(new(programmaticv1.AbortCompleted))
	case ResponseRunState:
		state, err := mapRunState(response.State.State)
		if err != nil {
			return nil, err
		}
		result := new(programmaticv1.RunStateResult)
		result.SetState(state)
		result.SetActiveCorrelationId(response.State.ActiveCorrelationID)
		wire.SetRunState(result)
	case ResponseMessages:
		entries, err := mapHistoryEntries(response.Messages)
		if err != nil {
			return nil, err
		}
		result := new(programmaticv1.MessagesResult)
		result.SetEntries(entries)
		wire.SetMessages(result)
	case ResponseRejected:
		command, err := mapCommandType(response.Rejection.Command)
		if err != nil {
			return nil, err
		}
		code, err := mapRejectionCode(response.Rejection.Code)
		if err != nil {
			return nil, err
		}
		rejected := new(programmaticv1.CommandRejected)
		rejected.SetCommand(command)
		rejected.SetCode(code)
		rejected.SetMessage(response.Rejection.Message)
		wire.SetRejected(rejected)
	case ResponseUnspecified:
		return nil, errors.New("map command response: unspecified response kind")
	default:
		return nil, fmt.Errorf("map command response: unknown response kind %d", response.Kind)
	}
	mapped := new(programmaticv1.OpenResponse)
	mapped.SetCorrelationId(response.CorrelationID)
	mapped.SetCommandResponse(wire)
	return mapped, nil
}

//nolint:gocyclo // The closed event union is mapped exhaustively.
func mapEvent(event AgentEvent) (*programmaticv1.OpenResponse, error) {
	eventType, err := mapAgentEventType(event.Type)
	if err != nil {
		return nil, err
	}
	wire := new(programmaticv1.AgentEvent)
	wire.SetType(eventType)
	wire.SetRunId(event.RunID)

	switch event.Type {
	case AgentEventAgentStart, AgentEventTurnStart, AgentEventMessageStart, AgentEventAgentSettled:
	case AgentEventModelContentStart, AgentEventModelTextDelta, AgentEventModelContentEnd:
		content, mapErr := mapModelContent(event.ModelContent)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetModelContent(content)
	case AgentEventToolCallStart, AgentEventToolCallDelta:
		preview, mapErr := mapToolCallPreview(event.ToolCallPreview)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetToolCallPreview(preview)
	case AgentEventToolCallEnd:
		call, mapErr := mapFinalToolCall(event.FinalToolCall)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetFinalToolCall(call)
	case AgentEventMessageEnd:
		response, mapErr := mapModelResponse(event.ModelResponse)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetModelResponse(response)
	case AgentEventToolExecutionStart:
		execution := new(programmaticv1.ToolExecution)
		execution.SetCallId(event.ToolExecution.CallID)
		execution.SetToolName(event.ToolExecution.ToolName)
		wire.SetToolExecution(execution)
	case AgentEventToolExecutionUpdate:
		progress, mapErr := mapToolProgress(event.ToolProgress)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetToolProgress(progress)
	case AgentEventToolExecutionEnd, AgentEventToolResult:
		result, mapErr := mapToolResult(event.ToolResult)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetToolResult(result)
	case AgentEventTurnEnd:
		turn, mapErr := mapTurnSummary(event.Turn)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetTurn(turn)
	case AgentEventAgentEnd:
		agent, mapErr := mapAgentSummary(event.Agent)
		if mapErr != nil {
			return nil, mapErr
		}
		wire.SetAgent(agent)
	case AgentEventUnspecified:
		return nil, errors.New("map agent event: unspecified event type")
	default:
		return nil, fmt.Errorf("map agent event: unknown event type %d", event.Type)
	}
	mapped := new(programmaticv1.OpenResponse)
	mapped.SetCorrelationId(event.CorrelationID)
	mapped.SetAgentEvent(wire)
	return mapped, nil
}

func mapHistoryEntries(entries []HistoryEntry) ([]*programmaticv1.HistoryEntry, error) {
	mapped := make([]*programmaticv1.HistoryEntry, 0, len(entries))
	for index := range entries {
		entry := &entries[index]
		wire := new(programmaticv1.HistoryEntry)
		switch entry.Kind {
		case HistoryEntryUser:
			user := new(programmaticv1.UserMessage)
			user.SetText(entry.UserText)
			wire.SetUser(user)
		case HistoryEntryModel:
			model, err := mapModelResponse(entry.Model)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", index, err)
			}
			wire.SetModel(model)
		case HistoryEntryToolResult:
			result, err := mapToolResult(entry.ToolResult)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", index, err)
			}
			wire.SetToolResult(result)
		case HistoryEntryUnspecified:
			return nil, fmt.Errorf("map history entry %d: unspecified entry kind", index)
		default:
			return nil, fmt.Errorf("map history entry %d: unknown entry kind %d", index, entry.Kind)
		}
		mapped = append(mapped, wire)
	}
	return mapped, nil
}

func mapModelContent(content ModelContent) (*programmaticv1.ModelContent, error) {
	kind, err := mapModelContentKind(content.Kind)
	if err != nil {
		return nil, err
	}
	position, err := mapPosition(content.Position)
	if err != nil {
		return nil, err
	}
	mapped := new(programmaticv1.ModelContent)
	mapped.SetKind(kind)
	mapped.SetPosition(position)
	mapped.SetText(content.Text)
	return mapped, nil
}

func mapToolCallPreview(preview ToolCallPreview) (*programmaticv1.ToolCallPreview, error) {
	position, err := mapPosition(preview.Position)
	if err != nil {
		return nil, err
	}
	fields := make([]*programmaticv1.ToolCallPreviewField, 0, len(preview.Fields))
	for index, field := range preview.Fields {
		mapped := new(programmaticv1.ToolCallPreviewField)
		mapped.SetName(field.Name)
		switch field.Kind {
		case ToolCallPreviewFieldComplete:
			value, valueErr := structpb.NewValue(field.Value)
			if valueErr != nil {
				return nil, fmt.Errorf("map tool call preview field %d value: %w", index, valueErr)
			}
			mapped.SetValue(value)
		case ToolCallPreviewFieldPrefix:
			mapped.SetPrefix(field.Prefix)
		case ToolCallPreviewFieldUnspecified:
			return nil, fmt.Errorf("map tool call preview field %d: unspecified content kind", index)
		default:
			return nil, fmt.Errorf("map tool call preview field %d: unknown content kind %d", index, field.Kind)
		}
		fields = append(fields, mapped)
	}
	mapped := new(programmaticv1.ToolCallPreview)
	mapped.SetCallId(preview.CallID)
	mapped.SetName(preview.Name)
	mapped.SetPosition(position)
	mapped.SetProvisional(preview.Provisional)
	mapped.SetFields(fields)
	return mapped, nil
}

func mapFinalToolCall(call FinalToolCall) (*programmaticv1.FinalToolCall, error) {
	position, err := mapPosition(call.Position)
	if err != nil {
		return nil, err
	}
	arguments, err := structpb.NewStruct(call.Arguments)
	if err != nil {
		return nil, fmt.Errorf("map final tool call arguments: %w", err)
	}
	mapped := new(programmaticv1.FinalToolCall)
	mapped.SetCallId(call.CallID)
	mapped.SetName(call.Name)
	mapped.SetPosition(position)
	mapped.SetArguments(arguments)
	return mapped, nil
}

func mapToolProgress(progress ToolProgress) (*programmaticv1.ToolProgress, error) {
	channel, err := mapProgressChannel(progress.Channel)
	if err != nil {
		return nil, err
	}
	mapped := new(programmaticv1.ToolProgress)
	mapped.SetChannel(channel)
	mapped.SetContent(progress.Content)
	return mapped, nil
}

func mapToolResult(result ToolResult) (*programmaticv1.ToolResult, error) {
	contents := make([]*programmaticv1.ToolResultContent, 0, len(result.Contents))
	for index, content := range result.Contents {
		mapped := new(programmaticv1.ToolResultContent)
		switch content.Kind {
		case ToolResultContentText:
			mapped.SetText(content.Text)
		case ToolResultContentImage:
			image := new(programmaticv1.ToolResultImage)
			image.SetMediaType(content.Image.MediaType)
			image.SetData(bytes.Clone(content.Image.Data))
			mapped.SetImage(image)
		case ToolResultContentUnspecified:
			return nil, fmt.Errorf("map tool result content %d: unspecified content kind", index)
		default:
			return nil, fmt.Errorf("map tool result content %d: unknown content kind %d", index, content.Kind)
		}
		contents = append(contents, mapped)
	}
	mapped := new(programmaticv1.ToolResult)
	mapped.SetCallId(result.CallID)
	mapped.SetToolName(result.ToolName)
	mapped.SetContents(contents)
	mapped.SetIsError(result.IsError)
	return mapped, nil
}

func mapModelResponse(response ModelResponse) (*programmaticv1.ModelResponse, error) {
	outcome, err := mapModelOutcome(response.Outcome)
	if err != nil {
		return nil, err
	}
	content := make([]*programmaticv1.ModelResponseItem, 0, len(response.Content))
	for index, item := range response.Content {
		mapped := new(programmaticv1.ModelResponseItem)
		switch item.Kind {
		case ModelResponseContentText:
			text := new(programmaticv1.FinalText)
			text.SetText(item.Text)
			mapped.SetText(text)
		case ModelResponseContentRefusal:
			text := new(programmaticv1.FinalText)
			text.SetText(item.Text)
			mapped.SetRefusal(text)
		case ModelResponseContentReasoning:
			text := new(programmaticv1.FinalText)
			text.SetText(item.Text)
			mapped.SetReasoning(text)
		case ModelResponseContentToolCall:
			call, mapErr := mapFinalToolCall(item.ToolCall)
			if mapErr != nil {
				return nil, fmt.Errorf("map model response content %d: %w", index, mapErr)
			}
			mapped.SetToolCall(call)
		case ModelResponseContentUnspecified:
			return nil, fmt.Errorf("map model response content %d: unspecified content kind", index)
		default:
			return nil, fmt.Errorf("map model response content %d: unknown content kind %d", index, item.Kind)
		}
		content = append(content, mapped)
	}
	usage := new(programmaticv1.ModelUsage)
	usage.SetInputTokens(response.Usage.InputTokens)
	usage.SetOutputTokens(response.Usage.OutputTokens)
	usage.SetCachedInputTokens(response.Usage.CachedInputTokens)
	usage.SetCacheWriteTokens(response.Usage.CacheWriteTokens)
	usage.SetReasoningTokens(response.Usage.ReasoningTokens)
	usage.SetTotalTokens(response.Usage.TotalTokens)
	diagnostics := make([]*programmaticv1.ModelDiagnostic, 0, len(response.Diagnostics))
	for _, diagnostic := range response.Diagnostics {
		mapped := new(programmaticv1.ModelDiagnostic)
		mapped.SetCode(diagnostic.Code)
		mapped.SetMessage(diagnostic.Message)
		diagnostics = append(diagnostics, mapped)
	}
	mapped := new(programmaticv1.ModelResponse)
	mapped.SetText(response.Text)
	mapped.SetOutcome(outcome)
	mapped.SetErrorMessage(response.ErrorMessage)
	mapped.SetProvider(response.Provider)
	mapped.SetModel(response.Model)
	if response.ResponseModel != nil {
		mapped.SetResponseModel(*response.ResponseModel)
	}
	mapped.SetResponseId(response.ResponseID)
	mapped.SetUsage(usage)
	mapped.SetDiagnostics(diagnostics)
	mapped.SetContent(content)
	return mapped, nil
}

func mapTurnSummary(turn TurnSummary) (*programmaticv1.TurnSummary, error) {
	response, err := mapModelResponse(turn.Response)
	if err != nil {
		return nil, err
	}
	results := make([]*programmaticv1.ToolResult, 0, len(turn.ToolResults))
	for index, result := range turn.ToolResults {
		mapped, mapErr := mapToolResult(result)
		if mapErr != nil {
			return nil, fmt.Errorf("map turn tool result %d: %w", index, mapErr)
		}
		results = append(results, mapped)
	}
	mapped := new(programmaticv1.TurnSummary)
	mapped.SetResponse(response)
	mapped.SetToolResults(results)
	return mapped, nil
}

func mapAgentSummary(agent AgentSummary) (*programmaticv1.AgentSummary, error) {
	outcome, err := mapRunOutcome(agent.Outcome)
	if err != nil {
		return nil, err
	}
	mapped := new(programmaticv1.AgentSummary)
	mapped.SetOutcome(outcome)
	mapped.SetErrorMessage(agent.ErrorMessage)
	return mapped, nil
}

func mapCommandType(kind CommandKind) (programmaticv1.CommandType, error) {
	switch kind {
	case CommandUnspecified:
		return programmaticv1.CommandType_COMMAND_TYPE_UNSPECIFIED, nil
	case CommandUserRequest:
		return programmaticv1.CommandType_COMMAND_TYPE_USER_REQUEST, nil
	case CommandAbort:
		return programmaticv1.CommandType_COMMAND_TYPE_ABORT, nil
	case CommandGetRunState:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_RUN_STATE, nil
	case CommandGetMessages:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_MESSAGES, nil
	default:
		return 0, fmt.Errorf("map command type: unknown value %d", kind)
	}
}

func mapRejectionCode(code RejectionCode) (programmaticv1.RejectionCode, error) {
	switch code {
	case RejectionInvalidArgument:
		return programmaticv1.RejectionCode_REJECTION_CODE_INVALID_ARGUMENT, nil
	case RejectionBusy:
		return programmaticv1.RejectionCode_REJECTION_CODE_BUSY, nil
	case RejectionNoActiveRun:
		return programmaticv1.RejectionCode_REJECTION_CODE_NO_ACTIVE_RUN, nil
	case RejectionCorrelationInUse:
		return programmaticv1.RejectionCode_REJECTION_CODE_CORRELATION_IN_USE, nil
	case RejectionInternal:
		return programmaticv1.RejectionCode_REJECTION_CODE_INTERNAL, nil
	case RejectionUnspecified:
		return 0, errors.New("map rejection code: unspecified value")
	default:
		return 0, fmt.Errorf("map rejection code: unknown value %d", code)
	}
}

func mapRunState(state RunState) (programmaticv1.RunState, error) {
	switch state {
	case RunStateIdle:
		return programmaticv1.RunState_RUN_STATE_IDLE, nil
	case RunStateRunning:
		return programmaticv1.RunState_RUN_STATE_RUNNING, nil
	case RunStateUnspecified:
		return 0, errors.New("map run state: unspecified value")
	default:
		return 0, fmt.Errorf("map run state: unknown value %d", state)
	}
}

//nolint:gocyclo // The closed event enum is mapped exhaustively.
func mapAgentEventType(eventType AgentEventType) (programmaticv1.AgentEventType, error) {
	switch eventType {
	case AgentEventAgentStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_START, nil
	case AgentEventTurnStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_START, nil
	case AgentEventMessageStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_START, nil
	case AgentEventModelContentStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MODEL_CONTENT_START, nil
	case AgentEventModelTextDelta:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MODEL_TEXT_DELTA, nil
	case AgentEventModelContentEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MODEL_CONTENT_END, nil
	case AgentEventToolCallStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_CALL_START, nil
	case AgentEventToolCallDelta:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_CALL_DELTA, nil
	case AgentEventToolCallEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_CALL_END, nil
	case AgentEventMessageEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END, nil
	case AgentEventToolExecutionStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_START, nil
	case AgentEventToolExecutionUpdate:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_UPDATE, nil
	case AgentEventToolExecutionEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END, nil
	case AgentEventToolResult:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT, nil
	case AgentEventTurnEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END, nil
	case AgentEventAgentEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_END, nil
	case AgentEventAgentSettled:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED, nil
	case AgentEventUnspecified:
		return 0, errors.New("map agent event type: unspecified value")
	default:
		return 0, fmt.Errorf("map agent event type: unknown value %d", eventType)
	}
}

func mapModelContentKind(kind ModelContentKind) (programmaticv1.ModelContentKind, error) {
	switch kind {
	case ModelContentText:
		return programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT, nil
	case ModelContentReasoning:
		return programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING, nil
	case ModelContentRefusal:
		return programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL, nil
	case ModelContentUnspecified:
		return 0, errors.New("map model content kind: unspecified value")
	default:
		return 0, fmt.Errorf("map model content kind: unknown value %d", kind)
	}
}

func mapProgressChannel(channel ProgressChannel) (programmaticv1.ProgressChannel, error) {
	switch channel {
	case ProgressChannelStatus:
		return programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STATUS, nil
	case ProgressChannelStdout:
		return programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT, nil
	case ProgressChannelStderr:
		return programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STDERR, nil
	case ProgressChannelUnspecified:
		return 0, errors.New("map progress channel: unspecified value")
	default:
		return 0, fmt.Errorf("map progress channel: unknown value %d", channel)
	}
}

func mapModelOutcome(outcome ModelOutcome) (programmaticv1.ModelOutcome, error) {
	switch outcome {
	case ModelOutcomeStop:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_STOP, nil
	case ModelOutcomeToolUse:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_TOOL_USE, nil
	case ModelOutcomeLength:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_LENGTH, nil
	case ModelOutcomeAborted:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_ABORTED, nil
	case ModelOutcomeFailed:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_FAILED, nil
	case ModelOutcomeUnspecified:
		return 0, errors.New("map model outcome: unspecified value")
	default:
		return 0, fmt.Errorf("map model outcome: unknown value %d", outcome)
	}
}

func mapRunOutcome(outcome RunOutcome) (programmaticv1.RunOutcome, error) {
	switch outcome {
	case RunOutcomeCompleted:
		return programmaticv1.RunOutcome_RUN_OUTCOME_COMPLETED, nil
	case RunOutcomeAborted:
		return programmaticv1.RunOutcome_RUN_OUTCOME_ABORTED, nil
	case RunOutcomeFailed:
		return programmaticv1.RunOutcome_RUN_OUTCOME_FAILED, nil
	case RunOutcomeUnspecified:
		return 0, errors.New("map run outcome: unspecified value")
	default:
		return 0, fmt.Errorf("map run outcome: unknown value %d", outcome)
	}
}

func mapPosition(position int) (int32, error) {
	if position < math.MinInt32 || position > math.MaxInt32 {
		return 0, fmt.Errorf("map position: value %d exceeds int32", position)
	}
	return int32(position), nil
}
