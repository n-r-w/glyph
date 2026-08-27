package programmatic

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

func mapResponse(response Response) (*programmaticv1.OpenResponse, error) {
	wire := new(programmaticv1.CommandResponse)
	if handled, err := mapSessionOrRejectionResponse(wire, response); handled {
		if err != nil {
			return nil, err
		}
		return wrapCommandResponse(response.CorrelationID, wire), nil
	}
	switch response.Kind {
	case ResponseUserRequestAccepted:
		wire.SetUserRequestAccepted(new(programmaticv1.UserRequestAccepted))
	case ResponseAbortCompleted:
		wire.SetAbortCompleted(new(programmaticv1.AbortCompleted))
	case ResponseRunState:
		if err := mapRunStateCommandResponse(wire, response.State); err != nil {
			return nil, err
		}
	case ResponseMessages:
		entries, err := mapHistoryEntries(response.Messages)
		if err != nil {
			return nil, err
		}
		result := new(programmaticv1.MessagesResult)
		result.SetEntries(entries)
		wire.SetMessages(result)
	case ResponseModels:
		if err := mapModelsCommandResponse(wire, response.Models); err != nil {
			return nil, err
		}
	case ResponseModelSelection:
		if err := mapModelSelectionCommandResponse(wire, response.Selection); err != nil {
			return nil, err
		}
	case ResponseSessionInfo, ResponseSessions, ResponseSessionEntries, ResponseRejected:
		return nil, errors.New("map command response: handled response was not mapped")
	case ResponseUnspecified:
		return nil, errors.New("map command response: unspecified response kind")
	default:
		return nil, fmt.Errorf("map command response: unknown response kind %d", response.Kind)
	}
	return wrapCommandResponse(response.CorrelationID, wire), nil
}

// mapSessionOrRejectionResponse isolates lifecycle and rejection payload mapping from the core response dispatch.
func mapSessionOrRejectionResponse(wire *programmaticv1.CommandResponse, response Response) (bool, error) {
	switch response.Kind {
	case ResponseSessionInfo:
		info, present := response.SessionInfo.Get()
		if !present {
			return true, errors.New("map session information: result is absent")
		}
		result := new(programmaticv1.SessionInfoResult)
		result.SetInfo(mapSessionInfo(info))
		wire.SetSessionInfo(result)
		return true, nil
	case ResponseSessions:
		result := new(programmaticv1.SessionsResult)
		result.SetSessions(lo.Map(response.Sessions, func(summary session.Summary, _ int) *programmaticv1.SessionSummary {
			return mapSessionSummary(summary)
		}))
		wire.SetSessions(result)
		return true, nil
	case ResponseSessionEntries:
		entries, err := mapSessionEntries(response.SessionEntries)
		if err != nil {
			return true, err
		}
		result := new(programmaticv1.SessionEntriesResult)
		result.SetEntries(entries)
		wire.SetSessionEntries(result)
		return true, nil
	case ResponseRejected:
		return true, mapRejectionCommandResponse(wire, response.Rejection)
	case ResponseUnspecified, ResponseUserRequestAccepted, ResponseAbortCompleted,
		ResponseRunState, ResponseMessages, ResponseModels, ResponseModelSelection:
		return false, nil
	default:
		return false, nil
	}
}

// mapRunStateCommandResponse maps one run-state response after response-kind dispatch.
func mapSessionInfo(info session.Info) *programmaticv1.SessionInfo {
	wire := new(programmaticv1.SessionInfo)
	wire.SetId(string(info.ID))
	if name, present := info.Name.Get(); present {
		wire.SetName(name)
	}
	wire.SetWorkingDirectory(info.WorkingDirectory)
	if path, present := info.StoragePath.Get(); present {
		wire.SetStoragePath(path)
	}
	wire.SetCreatedTime(timestamppb.New(info.CreatedAt))
	wire.SetUpdateTime(timestamppb.New(info.UpdatedAt))
	return wire
}

// mapSessionSummary preserves optional display text and lifecycle information in the public contract.
func mapSessionSummary(summary session.Summary) *programmaticv1.SessionSummary {
	wire := new(programmaticv1.SessionSummary)
	wire.SetInfo(mapSessionInfo(summary.Info))
	if text, present := summary.FirstUserText.Get(); present {
		wire.SetFirstUserText(text)
	}
	wire.SetTotalMessages(int64(summary.TotalMessages))
	return wire
}

func mapRunStateCommandResponse(
	wire *programmaticv1.CommandResponse,
	response mo.Option[RunStateResult],
) error {
	stateResult, ok := response.Get()
	if !ok {
		return errors.New("map command response: missing run state")
	}
	state, err := mapRunState(stateResult.State)
	if err != nil {
		return err
	}
	result := new(programmaticv1.RunStateResult)
	result.SetState(state)
	if activeCorrelationID, present := stateResult.ActiveCorrelationID.Get(); present {
		result.SetActiveCorrelationId(activeCorrelationID)
	}
	wire.SetRunState(result)
	return nil
}

// mapModelsCommandResponse maps the catalog and confirmed selection after response-kind dispatch.
func mapModelsCommandResponse(
	wire *programmaticv1.CommandResponse,
	response mo.Option[ModelsResult],
) error {
	modelsResult, ok := response.Get()
	if !ok {
		return errors.New("map command response: missing models result")
	}
	models, err := mapConfiguredModels(modelsResult.Models)
	if err != nil {
		return err
	}
	activeSelection, ok := modelsResult.ActiveSelection.Get()
	if !ok {
		return errors.New("map models response: missing active selection")
	}
	selection, err := mapModelSelection(activeSelection)
	if err != nil {
		return err
	}
	result := new(programmaticv1.ModelsResult)
	result.SetModels(models)
	result.SetActiveSelection(selection)
	wire.SetModels(result)
	return nil
}

// mapModelSelectionCommandResponse maps one confirmed selection after response-kind dispatch.
func mapModelSelectionCommandResponse(
	wire *programmaticv1.CommandResponse,
	selection mo.Option[model.Selection],
) error {
	selectionValue, ok := selection.Get()
	if !ok {
		return errors.New("map command response: missing model selection")
	}
	mapped, err := mapModelSelection(selectionValue)
	if err != nil {
		return err
	}
	result := new(programmaticv1.ModelSelectionResult)
	result.SetSelection(mapped)
	wire.SetModelSelection(result)
	return nil
}

// mapRejectionCommandResponse maps one rejection after response-kind dispatch.
func mapRejectionCommandResponse(
	wire *programmaticv1.CommandResponse,
	response mo.Option[Rejection],
) error {
	rejection, ok := response.Get()
	if !ok {
		return errors.New("map command response: missing rejection")
	}
	command, err := mapCommandType(rejection.Command)
	if err != nil {
		return err
	}
	code, err := mapRejectionCode(rejection.Code)
	if err != nil {
		return err
	}
	rejected := new(programmaticv1.CommandRejected)
	rejected.SetCommand(command)
	rejected.SetCode(code)
	rejected.SetMessage(rejection.Message)
	wire.SetRejected(rejected)
	return nil
}

func wrapCommandResponse(
	correlationID string,
	response *programmaticv1.CommandResponse,
) *programmaticv1.OpenResponse {
	mapped := new(programmaticv1.OpenResponse)
	mapped.SetCorrelationId(correlationID)
	mapped.SetCommandResponse(response)
	return mapped
}

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
	mapped := new(programmaticv1.OpenResponse)
	mapped.SetCorrelationId(event.CorrelationID)
	mapped.SetAgentEvent(wire)
	return mapped, nil
}

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
		AgentEventToolResult, AgentEventTurnEnd, AgentEventAgentEnd, AgentEventAgentSettled:
		return fmt.Errorf("map model event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map model event: unsupported event type %d", event.Type)
	}
	return nil
}

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
		AgentEventToolResult, AgentEventTurnEnd, AgentEventAgentEnd, AgentEventAgentSettled:
		return fmt.Errorf("map tool call event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map tool call event: unsupported event type %d", event.Type)
	}
	return nil
}

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
		AgentEventTurnEnd, AgentEventAgentEnd, AgentEventAgentSettled:
		return fmt.Errorf("map tool execution event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map tool execution event: unsupported event type %d", event.Type)
	}
	return nil
}

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
		AgentEventToolResult, AgentEventAgentSettled:
		return fmt.Errorf("map terminal event: unsupported event type %d", event.Type)
	default:
		return fmt.Errorf("map terminal event: unsupported event type %d", event.Type)
	}
	return nil
}

func mapSessionEntries(entries []SessionEntry) ([]*programmaticv1.SessionEntry, error) {
	return lo.MapErr(entries, func(entry SessionEntry, index int) (*programmaticv1.SessionEntry, error) {
		wire := new(programmaticv1.SessionEntry)
		wire.SetId(entry.ID)
		wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
		switch entry.Kind {
		case HistoryEntryUser:
			text, present := entry.UserText.Get()
			if !present {
				return nil, fmt.Errorf("map session entry %d: missing user payload", index)
			}
			content := new(programmaticv1.UserContent)
			content.SetText(text)
			user := new(programmaticv1.UserMessage)
			user.SetContent([]*programmaticv1.UserContent{content})
			wire.SetUser(user)
		case HistoryEntryModel:
			response, present := entry.Model.Get()
			if !present {
				return nil, fmt.Errorf("map session entry %d: missing model payload", index)
			}
			mapped, err := mapModelResponse(response)
			if err != nil {
				return nil, fmt.Errorf("map session entry %d: %w", index, err)
			}
			wire.SetModel(mapped)
		case HistoryEntryToolResult:
			result, present := entry.ToolResult.Get()
			if !present {
				return nil, fmt.Errorf("map session entry %d: missing tool result payload", index)
			}
			mapped, err := mapToolResult(result)
			if err != nil {
				return nil, fmt.Errorf("map session entry %d: %w", index, err)
			}
			wire.SetToolResult(mapped)
		case HistoryEntryUnspecified:
			return nil, fmt.Errorf("map session entry %d: unsupported kind %d", index, entry.Kind)
		default:
			return nil, fmt.Errorf("map session entry %d: unknown kind %d", index, entry.Kind)
		}
		return wire, nil
	})
}

func mapHistoryEntries(entries []HistoryEntry) ([]*programmaticv1.HistoryEntry, error) {
	return lo.MapErr(entries, func(entry HistoryEntry, index int) (*programmaticv1.HistoryEntry, error) {
		wire := new(programmaticv1.HistoryEntry)
		switch entry.Kind {
		case HistoryEntryUser:
			userText, ok := entry.UserText.Get()
			if !ok {
				return nil, fmt.Errorf("map history entry %d: missing user payload", index)
			}
			content := new(programmaticv1.UserContent)
			content.SetText(userText)
			user := new(programmaticv1.UserMessage)
			user.SetContent([]*programmaticv1.UserContent{content})
			wire.SetUser(user)
		case HistoryEntryModel:
			modelValue, ok := entry.Model.Get()
			if !ok {
				return nil, fmt.Errorf("map history entry %d: missing model payload", index)
			}
			modelResponse, err := mapModelResponse(modelValue)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", index, err)
			}
			wire.SetModel(modelResponse)
		case HistoryEntryToolResult:
			toolResult, ok := entry.ToolResult.Get()
			if !ok {
				return nil, fmt.Errorf("map history entry %d: missing tool result payload", index)
			}
			result, err := mapToolResult(toolResult)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", index, err)
			}
			wire.SetToolResult(result)
		case HistoryEntryUnspecified:
			return nil, fmt.Errorf("map history entry %d: unspecified entry kind", index)
		default:
			return nil, fmt.Errorf("map history entry %d: unknown entry kind %d", index, entry.Kind)
		}
		return wire, nil
	})
}

func mapModelContent(content ModelContent, requireText bool) (*programmaticv1.ModelContent, error) {
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
	if text, present := content.Text.Get(); present {
		mapped.SetText(text)
	} else if requireText {
		return nil, errors.New("map model text delta: text is missing")
	}
	return mapped, nil
}

func mapToolCallPreview(preview ToolCallPreview) (*programmaticv1.ToolCallPreview, error) {
	position, err := mapPosition(preview.Position)
	if err != nil {
		return nil, err
	}
	fields, err := lo.MapErr(
		preview.Fields,
		func(field ToolCallPreviewField, index int) (*programmaticv1.ToolCallPreviewField, error) {
			mapped := new(programmaticv1.ToolCallPreviewField)
			mapped.SetName(field.Name)
			switch field.Kind {
			case ToolCallPreviewFieldComplete:
				fieldValue, present := field.Value.Get()
				if !present {
					return nil, fmt.Errorf("map tool call preview field %d: value is missing", index)
				}
				value, valueErr := structpb.NewValue(fieldValue)
				if valueErr != nil {
					return nil, fmt.Errorf("map tool call preview field %d value: %w", index, valueErr)
				}
				mapped.SetValue(value)
			case ToolCallPreviewFieldPrefix:
				prefix, present := field.Prefix.Get()
				if !present {
					return nil, fmt.Errorf("map tool call preview field %d: prefix is missing", index)
				}
				mapped.SetPrefix(prefix)
			case ToolCallPreviewFieldUnspecified:
				return nil, fmt.Errorf("map tool call preview field %d: unspecified content kind", index)
			default:
				return nil, fmt.Errorf("map tool call preview field %d: unknown content kind %d", index, field.Kind)
			}
			return mapped, nil
		},
	)
	if err != nil {
		return nil, err
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
	contents, err := lo.MapErr(
		result.Contents,
		func(content ToolResultContent, index int) (*programmaticv1.ToolResultContent, error) {
			mapped := new(programmaticv1.ToolResultContent)
			switch content.Kind {
			case ToolResultContentText:
				text, present := content.Text.Get()
				if !present {
					return nil, fmt.Errorf("map tool result content %d: text is missing", index)
				}
				mapped.SetText(text)
			case ToolResultContentImage:
				imageValue, present := content.Image.Get()
				if !present {
					return nil, fmt.Errorf("map tool result content %d: image is missing", index)
				}
				image := new(programmaticv1.ToolResultImage)
				image.SetMediaType(imageValue.MediaType)
				image.SetData(bytes.Clone(imageValue.Data))
				mapped.SetImage(image)
			case ToolResultContentUnspecified:
				return nil, fmt.Errorf("map tool result content %d: unspecified content kind", index)
			default:
				return nil, fmt.Errorf("map tool result content %d: unknown content kind %d", index, content.Kind)
			}
			return mapped, nil
		},
	)
	if err != nil {
		return nil, err
	}
	mapped := new(programmaticv1.ToolResult)
	mapped.SetCallId(result.CallID)
	mapped.SetToolName(result.ToolName)
	mapped.SetContents(contents)
	mapped.SetIsError(result.IsError)
	return mapped, nil
}

func mapModelResponse(response ModelResponse) (*programmaticv1.ModelResponse, error) {
	outcome, err := mapRequiredModelOutcome(response.Outcome)
	if err != nil {
		return nil, err
	}
	content, err := lo.MapErr(response.Content, mapModelResponseItem)
	if err != nil {
		return nil, err
	}

	diagnostics := lo.Map(response.Diagnostics, func(diagnostic ModelDiagnostic, _ int) *programmaticv1.ModelDiagnostic {
		mapped := new(programmaticv1.ModelDiagnostic)
		mapped.SetCode(diagnostic.Code)
		mapped.SetMessage(diagnostic.Message)
		return mapped
	})
	mapped := new(programmaticv1.ModelResponse)
	mapped.SetText(response.Text)
	mapped.SetOutcome(outcome)
	if errorMessage, ok := response.ErrorMessage.Get(); ok {
		mapped.SetErrorMessage(errorMessage)
	}
	if provider, ok := response.Provider.Get(); ok {
		mapped.SetProvider(provider)
	}
	if configuredModel, ok := response.Model.Get(); ok {
		mapped.SetModel(configuredModel)
	}
	if responseModel, ok := response.ResponseModel.Get(); ok {
		mapped.SetResponseModel(responseModel)
	}
	if responseID, ok := response.ResponseID.Get(); ok {
		mapped.SetResponseId(responseID)
	}
	if usageValue, ok := response.Usage.Get(); ok {
		usage := new(programmaticv1.ModelUsage)
		usage.SetInputTokens(usageValue.InputTokens)
		usage.SetOutputTokens(usageValue.OutputTokens)
		usage.SetCachedInputTokens(usageValue.CachedInputTokens)
		usage.SetCacheWriteTokens(usageValue.CacheWriteTokens)
		usage.SetReasoningTokens(usageValue.ReasoningTokens)
		usage.SetTotalTokens(usageValue.TotalTokens)
		mapped.SetUsage(usage)
	}
	mapped.SetDiagnostics(diagnostics)
	mapped.SetContent(content)
	return mapped, nil
}

func mapModelResponseItem(item ModelResponseContent, index int) (*programmaticv1.ModelResponseItem, error) {
	mapped := new(programmaticv1.ModelResponseItem)
	switch item.Kind {
	case ModelResponseContentText, ModelResponseContentRefusal, ModelResponseContentReasoning:
		textValue, present := item.Text.Get()
		if !present {
			return nil, fmt.Errorf("map model response content %d: text is missing", index)
		}
		text := new(programmaticv1.FinalText)
		text.SetText(textValue)
		switch item.Kind {
		case ModelResponseContentText:
			mapped.SetText(text)
		case ModelResponseContentRefusal:
			mapped.SetRefusal(text)
		case ModelResponseContentReasoning:
			mapped.SetReasoning(text)
		case ModelResponseContentUnspecified, ModelResponseContentToolCall:
		}
	case ModelResponseContentToolCall:
		callValue, present := item.ToolCall.Get()
		if !present {
			return nil, fmt.Errorf("map model response content %d: tool call is missing", index)
		}
		call, err := mapFinalToolCall(callValue)
		if err != nil {
			return nil, fmt.Errorf("map model response content %d: %w", index, err)
		}
		mapped.SetToolCall(call)
	case ModelResponseContentUnspecified:
		return nil, fmt.Errorf("map model response content %d: unspecified content kind", index)
	default:
		return nil, fmt.Errorf("map model response content %d: unknown content kind %d", index, item.Kind)
	}
	return mapped, nil
}

func mapTurnSummary(turn TurnSummary) (*programmaticv1.TurnSummary, error) {
	response, err := mapModelResponse(turn.Response)
	if err != nil {
		return nil, err
	}
	results, err := lo.MapErr(turn.ToolResults, func(result ToolResult, index int) (*programmaticv1.ToolResult, error) {
		mapped, mapErr := mapToolResult(result)
		if mapErr != nil {
			return nil, fmt.Errorf("map turn tool result %d: %w", index, mapErr)
		}
		return mapped, nil
	})
	if err != nil {
		return nil, err
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
	if errorMessage, present := agent.ErrorMessage.Get(); present {
		mapped.SetErrorMessage(errorMessage)
	}
	return mapped, nil
}

func mapConfiguredModels(descriptors []model.Descriptor) ([]*programmaticv1.ConfiguredModel, error) {
	return lo.MapErr(descriptors, func(descriptor model.Descriptor, _ int) (*programmaticv1.ConfiguredModel, error) {
		choices, err := lo.MapErr(
			descriptor.ReasoningCapabilities.Choices,
			func(choice model.ReasoningChoice, _ int) (programmaticv1.ReasoningChoice, error) {
				return mapReasoningChoice(choice)
			},
		)
		if err != nil {
			return nil, err
		}
		defaultChoice, err := mapReasoningChoice(descriptor.ReasoningCapabilities.Default)
		if err != nil {
			return nil, err
		}
		capabilities := new(programmaticv1.ReasoningCapabilities)
		capabilities.SetSupported(descriptor.ReasoningCapabilities.Supported)
		capabilities.SetChoices(choices)
		capabilities.SetDefaultChoice(defaultChoice)
		configured := new(programmaticv1.ConfiguredModel)
		configured.SetProviderId(string(descriptor.Provider))
		configured.SetModelId(string(descriptor.Model))
		configured.SetReasoning(capabilities)
		return configured, nil
	})
}

func mapModelSelection(selection model.Selection) (*programmaticv1.ModelSelection, error) {
	level, err := mapReasoningChoice(selection.ReasoningChoice)
	if err != nil {
		return nil, err
	}
	mapped := new(programmaticv1.ModelSelection)
	mapped.SetProviderId(string(selection.Provider))
	mapped.SetModelId(string(selection.Model))
	mapped.SetReasoningChoice(level)
	return mapped, nil
}

func mapReasoningChoice(level model.ReasoningChoice) (programmaticv1.ReasoningChoice, error) {
	switch level {
	case model.ReasoningChoiceOff:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF, nil
	case model.ReasoningChoiceOn:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_ON, nil
	case model.ReasoningChoiceMinimal:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_MINIMAL, nil
	case model.ReasoningChoiceLow:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW, nil
	case model.ReasoningChoiceMedium:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_MEDIUM, nil
	case model.ReasoningChoiceHigh:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, nil
	case model.ReasoningChoiceXHigh:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_XHIGH, nil
	case model.ReasoningChoiceMax:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX, nil
	default:
		return 0, fmt.Errorf("map reasoning choice: unknown value %q", level)
	}
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
	case CommandGetModels:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_MODELS, nil
	case CommandSelectModel:
		return programmaticv1.CommandType_COMMAND_TYPE_SELECT_MODEL, nil
	case CommandSelectReasoningChoice:
		return programmaticv1.CommandType_COMMAND_TYPE_SELECT_REASONING_CHOICE, nil
	case CommandCreateSession:
		return programmaticv1.CommandType_COMMAND_TYPE_CREATE_SESSION, nil
	case CommandListSessions:
		return programmaticv1.CommandType_COMMAND_TYPE_LIST_SESSIONS, nil
	case CommandResumeSession:
		return programmaticv1.CommandType_COMMAND_TYPE_RESUME_SESSION, nil
	case CommandSetSessionName:
		return programmaticv1.CommandType_COMMAND_TYPE_SET_SESSION_NAME, nil
	case CommandGetSessionInfo:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_SESSION_INFO, nil
	case CommandGetSessionEntries:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_SESSION_ENTRIES, nil
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
	case RejectionNotFound:
		return programmaticv1.RejectionCode_REJECTION_CODE_NOT_FOUND, nil
	case RejectionReasoningUnsupported:
		return programmaticv1.RejectionCode_REJECTION_CODE_REASONING_UNSUPPORTED, nil
	case RejectionCredentialUnavailable:
		return programmaticv1.RejectionCode_REJECTION_CODE_CREDENTIAL_UNAVAILABLE, nil
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

// mapRequiredModelOutcome maps the required outcome at the Protobuf boundary.
func mapRequiredModelOutcome(outcome mo.Option[ModelOutcome]) (programmaticv1.ModelOutcome, error) {
	outcomeValue, ok := outcome.Get()
	if !ok {
		return 0, errors.New("map model response: missing outcome")
	}
	return mapModelOutcome(outcomeValue)
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
