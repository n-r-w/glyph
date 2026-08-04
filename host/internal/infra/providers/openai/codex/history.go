package codex

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// reasoningContext is the provider-owned opaque replay payload stored by Agent Core.
type reasoningContext struct {
	ID               string   `json:"id"`
	EncryptedContent string   `json:"encrypted_content"`
	Summary          []string `json:"summary"`
}

// buildInput converts complete projected history into ordered Responses input items.
func buildInput(history []agent.HistoryEntry) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(history))
	for entryIndex := range history {
		entry := &history[entryIndex]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			input = append(input, messageInput(responses.EasyInputMessageRoleUser, entry.User.Text))
		case agent.HistoryEntryModel:
			modelInput, err := buildModelInput(entry.Model)
			if err != nil {
				return nil, err
			}
			input = append(input, modelInput...)
		case agent.HistoryEntryToolResult:
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(
				entry.ToolResult.CallID,
				entry.ToolResult.Content,
			))
		}
	}
	return input, nil
}

// buildModelInput preserves model item order and ignores context owned by other providers.
func buildModelInput(response agent.ModelResponse) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(response.Items))
	for _, item := range response.Items {
		switch item.Kind {
		case agent.ModelItemText:
			input = append(input, messageInput(responses.EasyInputMessageRoleAssistant, item.Text))
		case agent.ModelItemProviderContext:
			if item.ProviderContext.ProviderID != ProviderID {
				continue
			}
			reasoning, err := reasoningInput(item.ProviderContext.Payload)
			if err != nil {
				return nil, err
			}
			input = append(input, reasoning)
		case agent.ModelItemToolCall:
			arguments, err := json.Marshal(item.ToolCall.Arguments)
			if err != nil {
				return nil, fmt.Errorf("encode Codex tool-call arguments: %w", err)
			}
			input = append(input, responses.ResponseInputItemParamOfFunctionCall(
				string(arguments),
				item.ToolCall.ID,
				item.ToolCall.Name,
			))
		}
	}
	return input, nil
}

// reasoningInput validates stateless replay data and maps its summaries.
func reasoningInput(payload []byte) (responses.ResponseInputItemUnionParam, error) {
	var contextValue reasoningContext
	if err := json.Unmarshal(payload, &contextValue); err != nil {
		return responses.ResponseInputItemUnionParam{}, errors.New("OpenAI Codex reasoning context is malformed")
	}
	if contextValue.ID == "" || contextValue.EncryptedContent == "" {
		return responses.ResponseInputItemUnionParam{}, errors.New(
			"OpenAI Codex stateless continuation requires encrypted reasoning",
		)
	}
	summary := make([]responses.ResponseReasoningItemSummaryParam, len(contextValue.Summary))
	for index, text := range contextValue.Summary {
		//nolint:exhaustruct // SDK sets the fixed summary type during JSON encoding.
		summary[index] = responses.ResponseReasoningItemSummaryParam{Text: text}
	}
	reasoning := responses.ResponseInputItemParamOfReasoning(contextValue.ID, summary)
	reasoning.OfReasoning.EncryptedContent = param.NewOpt(contextValue.EncryptedContent)
	return reasoning, nil
}

// messageInput creates one ordered message item without using request string shorthand.
func messageInput(role responses.EasyInputMessageRole, text string) responses.ResponseInputItemUnionParam {
	message := responses.ResponseInputItemParamOfMessage(text, role)
	message.OfMessage.Type = responses.EasyInputMessageTypeMessage
	return message
}

// buildTools maps provider-neutral schemas into strict Codex function tools.
func buildTools(descriptors []tool.Descriptor) ([]responses.ToolUnionParam, error) {
	tools := make([]responses.ToolUnionParam, 0, len(descriptors))
	for _, descriptor := range descriptors {
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return nil, fmt.Errorf("decode schema for Codex tool %q: %w", descriptor.Name, err)
		}
		//nolint:exhaustruct // Other SDK tool variants are intentionally omitted.
		toolParam := responses.ToolUnionParam{
			//nolint:exhaustruct // Optional Codex function fields use SDK zero values.
			OfFunction: &responses.FunctionToolParam{
				Name:        descriptor.Name,
				Description: param.NewOpt(descriptor.Description),
				Parameters:  schema,
				Strict:      param.NewOpt(true),
			},
		}
		tools = append(tools, toolParam)
	}
	return tools, nil
}
