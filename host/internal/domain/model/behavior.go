package model

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Valid reports whether the reasoning choice is part of the provider-neutral contract.
func (choice ReasoningChoice) Valid() bool {
	switch choice {
	case ReasoningChoiceOff, ReasoningChoiceOn, ReasoningChoiceMinimal, ReasoningChoiceLow,
		ReasoningChoiceMedium, ReasoningChoiceHigh, ReasoningChoiceXHigh, ReasoningChoiceMax:
		return true
	default:
		return false
	}
}

// Effort reports whether the choice selects an explicit reasoning effort.
func (choice ReasoningChoice) Effort() bool {
	switch choice {
	case ReasoningChoiceMinimal, ReasoningChoiceLow, ReasoningChoiceMedium, ReasoningChoiceHigh,
		ReasoningChoiceXHigh, ReasoningChoiceMax:
		return true
	case ReasoningChoiceOff, ReasoningChoiceOn:
		return false
	default:
		return false
	}
}

// Valid reports whether the descriptor satisfies provider-neutral model invariants.
func (descriptor Descriptor) Valid() bool {
	return descriptor.Provider != "" && descriptor.Model != "" && descriptor.validInput() &&
		descriptor.ContextWindow > 0 && descriptor.MaxTokens > 0 &&
		descriptor.MaxTokens <= descriptor.ContextWindow && descriptor.ReasoningCapabilities.Valid()
}

// validInput reports whether input modalities form a unique closed set that includes text.
func (descriptor Descriptor) validInput() bool {
	if len(descriptor.Input) == 0 {
		return false
	}
	modalities := make(map[InputModality]struct{}, len(descriptor.Input))
	for _, modality := range descriptor.Input {
		if modality != InputModalityText && modality != InputModalityImage {
			return false
		}
		if _, duplicate := modalities[modality]; duplicate {
			return false
		}
		modalities[modality] = struct{}{}
	}
	_, containsText := modalities[InputModalityText]
	return containsText
}

// Valid reports whether reasoning choices form a unique closed set containing the default.
func (capabilities ReasoningCapabilities) Valid() bool {
	if len(capabilities.Choices) == 0 || !slices.Contains(capabilities.Choices, capabilities.Default) {
		return false
	}
	choices := make(map[ReasoningChoice]struct{}, len(capabilities.Choices))
	for _, choice := range capabilities.Choices {
		if !choice.Valid() {
			return false
		}
		if _, duplicate := choices[choice]; duplicate {
			return false
		}
		choices[choice] = struct{}{}
	}
	return true
}

// Clone returns a deep copy of the descriptor.
func (descriptor Descriptor) Clone() Descriptor {
	descriptor.Input = slices.Clone(descriptor.Input)
	descriptor.ReasoningCapabilities.Choices = slices.Clone(descriptor.ReasoningCapabilities.Choices)
	descriptor.Pricing = descriptor.Pricing.MapValue(func(pricing Pricing) Pricing {
		pricing.Tiers = slices.Clone(pricing.Tiers)
		return pricing
	})
	return descriptor
}

// Text returns ordered text blocks joined with the requested separator.
func (message Message) Text(separator string) string {
	parts := make([]string, 0, len(message.Content))
	for _, content := range message.Content {
		if content.Kind == InputContentText {
			if text, present := content.Text.Get(); present {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, separator)
}

// Clone returns a deep copy of the message.
func (message Message) Clone() Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = message.Content[index].Data.MapValue(bytes.Clone)
	}
	return message
}

// Valid reports whether the outcome is part of the provider-neutral contract.
func (outcome Outcome) Valid() bool {
	return outcome >= OutcomeStop && outcome <= OutcomeFailed
}

// Streamed reports whether content of this kind can arrive incrementally.
func (kind ContentKind) Streamed() bool {
	return kind == ContentText || kind == ContentRefusal || kind == ContentReasoning
}

// ValidateShape validates the content discriminator and payload field presence.
func (content Content) ValidateShape() error {
	hasText := content.Text.IsSome()
	hasProviderContext := content.ProviderContext.IsSome()
	hasToolCall := content.ToolCall.IsSome()
	switch content.Kind {
	case ContentText, ContentRefusal:
		if !hasText || hasProviderContext || hasToolCall {
			return fmt.Errorf("invalid payload fields for kind %d", content.Kind)
		}
	case ContentReasoning:
		if !hasText && !hasProviderContext || hasToolCall {
			return fmt.Errorf("invalid payload fields for kind %d", content.Kind)
		}
	case ContentToolCall:
		if hasText || hasProviderContext || !hasToolCall {
			return fmt.Errorf("invalid payload fields for kind %d", content.Kind)
		}
	default:
		return fmt.Errorf("unknown kind %d", content.Kind)
	}
	return nil
}

// CompatibleWith reports whether opaque context can be replayed by the target contract.
func (source ProviderContextSource) CompatibleWith(target ProviderContextSource) bool {
	if source.ProviderID != target.ProviderID || source.API != target.API {
		return false
	}
	if source.Model == target.Model {
		return true
	}
	sourceKey, sourceHasKey := source.CompatibilityKey.Get()
	targetKey, targetHasKey := target.CompatibilityKey.Get()
	return sourceHasKey && targetHasKey && sourceKey != "" && sourceKey == targetKey
}

// Clone returns a deep copy of the tool call.
func (call ToolCall) Clone() ToolCall {
	call.Arguments = cloneJSONMap(call.Arguments)
	return call
}

// Clone returns a deep copy of the preview field.
func (field ToolCallPreviewField) Clone() ToolCallPreviewField {
	field.Value = field.Value.MapValue(cloneJSONValue)
	return field
}

// Clone returns a deep copy of the tool-call preview.
func (preview ToolCallPreview) Clone() ToolCallPreview {
	preview.Fields = slices.Clone(preview.Fields)
	for index := range preview.Fields {
		preview.Fields[index] = preview.Fields[index].Clone()
	}
	return preview
}

// Normalize converts provider totals into disjoint nonnegative buckets.
func (usage Usage) Normalize() Usage {
	cacheRead := max(int64(0), usage.CachedInputTokens)
	cacheWrite := max(int64(0), usage.CacheWriteTokens)
	output := max(int64(0), usage.OutputTokens)
	// Provider input includes both cache buckets, so only the remainder is uncached input.
	input := max(int64(0), usage.InputTokens-cacheRead-cacheWrite)
	// Reasoning is output detail. It cannot exceed output and never increases the total.
	reasoning := min(max(int64(0), usage.ReasoningTokens), output)
	return Usage{
		InputTokens:       input,
		OutputTokens:      output,
		CachedInputTokens: cacheRead,
		CacheWriteTokens:  cacheWrite,
		ReasoningTokens:   reasoning,
		TotalTokens:       input + output + cacheRead + cacheWrite,
	}
}

// Text returns concatenated visible text and refusal blocks.
func (response Response) Text() string {
	var builder strings.Builder
	for index := range response.Content {
		item := response.Content[index]
		if item.Kind == ContentText || item.Kind == ContentRefusal {
			if text, present := item.Text.Get(); present {
				builder.WriteString(text)
			}
		}
	}
	return builder.String()
}

// ValidateTerminalContent validates every terminal content discriminator.
func (response Response) ValidateTerminalContent() error {
	for position := range response.Content {
		content := response.Content[position]
		if err := content.ValidateShape(); err != nil {
			return fmt.Errorf("terminal model content %d: %w", position, err)
		}
		if content.Kind.Streamed() && !content.Final {
			return fmt.Errorf("terminal model content %d is not final", position)
		}
	}
	return nil
}

// Clone returns a deep copy of the response.
func (response Response) Clone() Response {
	response.Content = slices.Clone(response.Content)
	for index := range response.Content {
		response.Content[index].ProviderContext = response.Content[index].ProviderContext.MapValue(
			func(value ProviderContext) ProviderContext {
				value.Payload = bytes.Clone(value.Payload)
				return value
			},
		)
		response.Content[index].ToolCall = response.Content[index].ToolCall.MapValue(ToolCall.Clone)
	}
	response.Diagnostics = slices.Clone(response.Diagnostics)
	return response
}

// cloneJSONMap returns a deep copy of one JSON object.
func cloneJSONMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := maps.Clone(source)
	for key, value := range cloned {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

// cloneJSONValue returns a deep copy of JSON containers and byte slices.
func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONMap(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index := range cloned {
			cloned[index] = cloneJSONValue(cloned[index])
		}
		return cloned
	case []byte:
		return bytes.Clone(typed)
	default:
		return typed
	}
}
