package programmatic

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

func mapSessionEntries(entries []SessionEntry) ([]*programmaticv1.SessionEntry, error) {
	return lo.MapErr(entries, func(entry SessionEntry, index int) (*programmaticv1.SessionEntry, error) {
		wire := new(programmaticv1.SessionEntry)
		wire.SetId(entry.ID)
		wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
		switch entry.Kind {
		case HistoryEntryUser:
			user, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("map session entry %d: missing user payload", index)
			}
			mapped, err := mapUserMessage(user)
			if err != nil {
				return nil, fmt.Errorf("map session entry %d: %w", index, err)
			}
			wire.SetUser(mapped)
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
		if cost, present := entry.EstimatedCost.Get(); present {
			wire.SetEstimatedCost(mapEstimatedCost(cost))
		}
		return wire, nil
	})
}

func mapHistoryEntries(entries []HistoryEntry) ([]*programmaticv1.HistoryEntry, error) {
	return lo.MapErr(entries, func(entry HistoryEntry, index int) (*programmaticv1.HistoryEntry, error) {
		wire := new(programmaticv1.HistoryEntry)
		switch entry.Kind {
		case HistoryEntryUser:
			user, ok := entry.User.Get()
			if !ok {
				return nil, fmt.Errorf("map history entry %d: missing user payload", index)
			}
			mapped, err := mapUserMessage(user)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", index, err)
			}
			wire.SetUser(mapped)
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

func mapUserMessage(message model.Message) (*programmaticv1.UserMessage, error) {
	content, err := lo.MapErr(message.Content, func(
		item model.InputContent,
		index int,
	) (*programmaticv1.UserContent, error) {
		wire := new(programmaticv1.UserContent)
		switch item.Kind {
		case model.InputContentText:
			text, present := item.Text.Get()
			if !present || item.MediaType.IsSome() || item.Data.IsSome() {
				return nil, fmt.Errorf("map user content %d: invalid text payload", index)
			}
			wire.SetText(text)
		case model.InputContentImage:
			mediaType, hasMediaType := item.MediaType.Get()
			data, hasData := item.Data.Get()
			if item.Text.IsSome() || !hasMediaType || !hasData {
				return nil, fmt.Errorf("map user content %d: invalid image payload", index)
			}
			image := programmaticv1.UserImage_builder{
				MediaType: new(mediaType), Data: nil,
			}.Build()
			image.SetData(bytes.Clone(data))
			wire.SetImage(image)
		default:
			return nil, fmt.Errorf("map user content %d: unknown kind %d", index, item.Kind)
		}
		return wire, nil
	})
	if err != nil {
		return nil, err
	}
	wire := new(programmaticv1.UserMessage)
	wire.SetContent(content)
	return wire, nil
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
		setCommonUsage(
			usage,
			usageValue.InputTokens,
			usageValue.OutputTokens,
			usageValue.CacheWriteTokens,
			usageValue.ReasoningTokens,
			usageValue.TotalTokens,
		)
		usage.SetCachedInputTokens(usageValue.CachedInputTokens)
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
