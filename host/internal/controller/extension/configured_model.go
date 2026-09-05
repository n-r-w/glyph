package extension

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// configuredRequest contains validated provider-neutral input for one explicit model request.
type configuredRequest struct {
	// selection identifies the exact configured model and reasoning choice.
	selection model.Selection
	// instructions contains the request instructions and can be empty.
	instructions string
	// history contains ordered user and assistant text entries.
	history []agent.HistoryEntry
}

// mapConfiguredRequest validates and maps the public text-only request.
func mapConfiguredRequest(request *extensionpb.ConfiguredModelRequest) (configuredRequest, error) {
	if request == nil {
		return configuredRequest{}, errors.New("configured model request is required")
	}
	selection := request.GetSelection()
	if selection == nil || selection.GetProviderId() == "" || selection.GetModelId() == "" ||
		selection.GetReasoningChoice() == "" {
		return configuredRequest{}, errors.New("complete configured model selection is required")
	}
	messages := request.GetMessages()
	if len(messages) == 0 {
		return configuredRequest{}, errors.New("configured model request requires at least one message")
	}
	history := make([]agent.HistoryEntry, len(messages))
	for index, message := range messages {
		if message == nil || message.GetText() == "" {
			return configuredRequest{}, fmt.Errorf("configured model message %d requires nonempty text", index)
		}
		switch message.GetRole() {
		case extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER:
			history[index] = agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(message.GetText())),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			}
		case extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_ASSISTANT:
			history[index] = agent.HistoryEntry{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(model.Response{
					Content: []model.Content{{
						Kind: model.ContentText, Text: mo.Some(message.GetText()), Final: true,
						ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
					}},
					Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.None[model.Usage](), Diagnostics: nil,
				}), ToolResult: mo.None[agent.ToolResult](),
			}
		case extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_UNSPECIFIED:
			return configuredRequest{}, fmt.Errorf("configured model message %d role is unspecified", index)
		default:
			return configuredRequest{}, fmt.Errorf(
				"configured model message %d role %d is unknown",
				index,
				message.GetRole(),
			)
		}
	}
	return configuredRequest{
		selection: model.Selection{
			Provider: model.ProviderID(selection.GetProviderId()), Model: model.ID(selection.GetModelId()),
			ReasoningChoice: model.ReasoningChoice(selection.GetReasoningChoice()),
		},
		instructions: request.GetInstructions(),
		history:      history,
	}, nil
}

// mapConfiguredResponse projects terminal content without opaque provider reasoning context.
func mapConfiguredResponse(response model.Response) (*extensionpb.ConfiguredModelResult, error) {
	if err := response.ValidateTerminalContent(); err != nil {
		return nil, fmt.Errorf("map configured model response: %w", err)
	}
	content := make([]*extensionpb.ConfiguredModelContent, 0, len(response.Content))
	for index := range response.Content {
		mapped, visible, err := mapConfiguredContent(response.Content[index])
		if err != nil {
			return nil, fmt.Errorf("map configured model response content %d: %w", index, err)
		}
		if visible {
			content = append(content, mapped)
		}
	}
	result := new(extensionpb.ConfiguredModelResult)
	result.SetContent(content)
	if outcome, present := response.Outcome.Get(); present {
		mapped, err := mapConfiguredOutcome(outcome)
		if err != nil {
			return nil, err
		}
		result.SetOutcome(mapped)
	}
	if value, present := response.ErrorMessage.Get(); present {
		result.SetErrorMessage(value)
	}
	if value, present := response.Provider.Get(); present {
		result.SetProviderId(string(value))
	}
	if value, present := response.Model.Get(); present {
		result.SetModelId(string(value))
	}
	if value, present := response.ResponseModel.Get(); present {
		result.SetResponseModelId(string(value))
	}
	if value, present := response.ResponseID.Get(); present {
		result.SetResponseId(value)
	}
	if usage, present := response.Usage.Get(); present {
		result.SetUsage(extensionpb.ConfiguredModelUsage_builder{
			InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
			CachedInputTokens: new(usage.CachedInputTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
			ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
		}.Build())
	}
	diagnostics := make([]*extensionpb.ConfiguredModelDiagnostic, len(response.Diagnostics))
	for index, diagnostic := range response.Diagnostics {
		diagnostics[index] = extensionpb.ConfiguredModelDiagnostic_builder{
			Code: new(diagnostic.Code), Message: new(diagnostic.Message),
		}.Build()
	}
	result.SetDiagnostics(diagnostics)
	return result, nil
}

// mapConfiguredContent maps one public terminal block and excludes provider-context-only reasoning.
func mapConfiguredContent(item model.Content) (*extensionpb.ConfiguredModelContent, bool, error) {
	mapped := new(extensionpb.ConfiguredModelContent)
	switch item.Kind {
	case model.ContentText, model.ContentRefusal, model.ContentReasoning:
		text, present := item.Text.Get()
		if !present {
			if item.Kind == model.ContentReasoning && item.ProviderContext.IsSome() {
				return nil, false, nil
			}
			return nil, false, errors.New("public text is missing")
		}
		value := extensionpb.ConfiguredModelText_builder{Text: new(text)}.Build()
		switch item.Kind {
		case model.ContentText:
			mapped.SetText(value)
		case model.ContentRefusal:
			mapped.SetRefusal(value)
		case model.ContentReasoning:
			mapped.SetReasoning(value)
		case model.ContentToolCall:
		}
	case model.ContentToolCall:
		call, present := item.ToolCall.Get()
		if !present {
			return nil, false, errors.New("tool call is missing")
		}
		arguments, err := json.Marshal(call.Arguments)
		if err != nil {
			return nil, false, fmt.Errorf("encode tool call arguments: %w", err)
		}
		mapped.SetToolCall(extensionpb.ConfiguredModelToolCall_builder{
			Id: new(call.ID), Name: new(call.Name), ArgumentsJson: arguments,
		}.Build())
	default:
		return nil, false, fmt.Errorf("unknown content kind %d", item.Kind)
	}
	return mapped, true, nil
}

// mapConfiguredOutcome maps one closed provider-neutral terminal outcome.
func mapConfiguredOutcome(outcome model.Outcome) (extensionpb.ConfiguredModelOutcome, error) {
	switch outcome {
	case model.OutcomeStop:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP, nil
	case model.OutcomeToolUse:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_TOOL_USE, nil
	case model.OutcomeLength:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_LENGTH, nil
	case model.OutcomeAborted:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_ABORTED, nil
	case model.OutcomeFailed:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_FAILED, nil
	default:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_UNSPECIFIED,
			fmt.Errorf("map configured model response: unknown outcome %d", outcome)
	}
}
