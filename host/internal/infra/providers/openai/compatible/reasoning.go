package compatible

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// reasoningFormat identifies a private OpenAI-compatible reasoning dialect.
type reasoningFormat string

const (
	// reasoningFormatOpenAIChat uses OpenAI Chat Completions reasoning fields.
	reasoningFormatOpenAIChat reasoningFormat = "openai-chat"
	// reasoningFormatOpenRouter uses OpenRouter Chat Completions reasoning fields.
	reasoningFormatOpenRouter reasoningFormat = "openrouter"
	// reasoningField identifies provider reasoning data.
	reasoningField = "reasoning"
	// reasoningEffortField identifies provider reasoning effort.
	reasoningEffortField = "effort"
	// reasoningDetailTypeText identifies visible reasoning detail text.
	reasoningDetailTypeText = "reasoning.text"
	// reasoningDetailFormatField identifies provider reasoning detail format.
	reasoningDetailFormatField = "format"
	// responseItemTypeFunctionCall identifies a provider tool-call output item.
	responseItemTypeFunctionCall = "function_call"
	// responseItemTypeReasoning identifies a provider reasoning output item.
	responseItemTypeReasoning = "reasoning"
	// responseReasoningSummaryDelta identifies a reasoning-summary stream delta.
	responseReasoningSummaryDelta = "response.reasoning_summary_text.delta"
)

// reasoningDetail preserves one opaque OpenRouter reasoning detail object.
type reasoningDetail map[string]jsontext.Value

// parseReasoningFormat validates one adapter-owned reasoning format against its API.
func parseReasoningFormat(raw string, api API, reasoningConfigured bool) (reasoningFormat, error) {
	if !reasoningConfigured {
		return "", nil
	}
	if api == APIResponses {
		if raw != "" {
			return "", fmt.Errorf("responses reasoning does not accept format %q", raw)
		}
		return "", nil
	}
	format := reasoningFormat(raw)
	switch format {
	case reasoningFormatOpenAIChat, reasoningFormatOpenRouter:
		return format, nil
	case "":
		return "", errors.New("chat Completions reasoning requires a format")
	default:
		return "", fmt.Errorf("reasoning format %q is unsupported", raw)
	}
}

// applyChatReasoningControl maps a provider-neutral choice to format-specific request fields.
func applyChatReasoningControl(
	params *openai.ChatCompletionNewParams,
	format reasoningFormat,
	choice model.ReasoningChoice,
) error {
	switch format {
	case "":
		return nil
	case reasoningFormatOpenAIChat:
		switch choice {
		case model.ReasoningChoiceOn:
			return nil
		case model.ReasoningChoiceOff:
			params.ReasoningEffort = shared.ReasoningEffortNone
		case model.ReasoningChoiceMinimal, model.ReasoningChoiceLow, model.ReasoningChoiceMedium,
			model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
			params.ReasoningEffort = shared.ReasoningEffort(choice)
		default:
			return errors.New("OpenAI-compatible reasoning choice is invalid")
		}
		return nil
	case reasoningFormatOpenRouter:
		var reasoning map[string]any
		switch choice {
		case model.ReasoningChoiceOn:
			reasoning = map[string]any{"enabled": true}
		case model.ReasoningChoiceOff:
			reasoning = map[string]any{reasoningEffortField: "none"}
		case model.ReasoningChoiceMinimal, model.ReasoningChoiceLow, model.ReasoningChoiceMedium,
			model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
			reasoning = map[string]any{reasoningEffortField: choice}
		default:
			return errors.New("OpenAI-compatible reasoning choice is invalid")
		}
		params.SetExtraFields(map[string]any{reasoningField: reasoning})
		return nil
	default:
		return fmt.Errorf("reasoning format %q is unsupported", format)
	}
}

// usesChatReasoning reports whether the format has native Chat Completions reasoning fields.
func usesChatReasoning(format reasoningFormat) bool {
	return format == reasoningFormatOpenAIChat || format == reasoningFormatOpenRouter
}

// chatReasoningDelta extracts provider-visible reasoning text from one stream chunk.
func chatReasoningDelta(format reasoningFormat, delta openai.ChatCompletionChunkChoiceDelta) (string, error) {
	if !usesChatReasoning(format) {
		return "", nil
	}
	field, ok := delta.JSON.ExtraFields[reasoningField]
	if !ok {
		return "", nil
	}
	var reasoning string
	if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err != nil {
		return "", fmt.Errorf("decode Chat Completions reasoning delta: %w", err)
	}
	return reasoning, nil
}

// openRouterReasoningDetailsDelta extracts opaque detail objects from one stream chunk.
func openRouterReasoningDetailsDelta(
	format reasoningFormat,
	delta openai.ChatCompletionChunkChoiceDelta,
) ([]reasoningDetail, error) {
	if format != reasoningFormatOpenRouter {
		return nil, nil
	}
	field, ok := delta.JSON.ExtraFields["reasoning_details"]
	if !ok {
		return nil, nil
	}
	var details []reasoningDetail
	if err := json.Unmarshal([]byte(field.Raw()), &details); err != nil {
		return nil, fmt.Errorf("decode OpenRouter reasoning_details delta: %w", err)
	}
	for index, detail := range details {
		if detail == nil {
			return nil, fmt.Errorf("decode OpenRouter reasoning_details delta: detail %d is not an object", index)
		}
	}
	return details, nil
}

// appendOpenRouterReasoningDetails joins consecutive text and summary fragments and preserves other fields.
func appendOpenRouterReasoningDetails(current, incoming []reasoningDetail) []reasoningDetail {
	for _, detail := range incoming {
		detailType, typePresent := reasoningDetailString(detail, "type")
		contentField := ""
		switch detailType {
		case reasoningDetailTypeText:
			contentField = "text"
		case "reasoning.summary":
			contentField = "summary"
		}
		if typePresent && contentField != "" && len(current) != 0 {
			last := current[len(current)-1]
			lastType, lastTypePresent := reasoningDetailString(last, "type")
			lastContent, lastContentPresent := reasoningDetailString(last, contentField)
			nextContent, nextContentPresent := reasoningDetailString(detail, contentField)
			if lastTypePresent && lastType == detailType && lastContentPresent && nextContentPresent {
				for key, value := range detail {
					if _, exists := last[key]; !exists {
						last[key] = bytes.Clone(value)
					}
				}
				fillOpenRouterReasoningDetailMetadata(last, detail, detailType)
				last[contentField], _ = json.Marshal(lastContent + nextContent)
				continue
			}
		}
		current = append(current, cloneReasoningDetail(detail))
	}
	return current
}

// fillOpenRouterReasoningDetailMetadata fills common fields that arrive after the first text fragment.
func fillOpenRouterReasoningDetailMetadata(target, source reasoningDetail, detailType string) {
	fields := []string{"id", reasoningDetailFormatField, "index"}
	if detailType == reasoningDetailTypeText {
		fields = append(fields, "signature")
	}
	for _, field := range fields {
		if !reasoningDetailFieldMissing(target, field) || reasoningDetailFieldMissing(source, field) {
			continue
		}
		target[field] = bytes.Clone(source[field])
	}
}

// reasoningDetailFieldMissing identifies nullable metadata and empty string metadata from partial chunks.
func reasoningDetailFieldMissing(detail reasoningDetail, field string) bool {
	raw, present := detail[field]
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	if field != reasoningDetailFormatField && field != "signature" {
		return false
	}
	value, valid := reasoningDetailString(detail, field)
	return !valid || value == ""
}

// reasoningDetailString reads one string field used to assemble streamed detail fragments.
func reasoningDetailString(detail reasoningDetail, field string) (string, bool) {
	raw, present := detail[field]
	if !present {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// cloneReasoningDetail isolates mutable raw JSON fields from the stream decoder.
func cloneReasoningDetail(detail reasoningDetail) reasoningDetail {
	cloned := make(reasoningDetail, len(detail))
	for key, value := range detail {
		cloned[key] = bytes.Clone(value)
	}
	return cloned
}

// openRouterProviderContext serializes assembled details for opaque persistence and replay.
func openRouterProviderContext(
	details []reasoningDetail,
	source model.ProviderContextSource,
) (model.ProviderContext, error) {
	payload, err := json.Marshal(details)
	if err != nil {
		return model.ProviderContext{}, fmt.Errorf("encode OpenRouter reasoning_details: %w", err)
	}
	return model.ProviderContext{Source: source, Payload: payload}, nil
}

// openRouterReplayDetails returns compatible opaque details for one assistant content block.
func openRouterReplayDetails(
	content model.Content,
	format reasoningFormat,
	target model.ProviderContextSource,
) ([]jsontext.Value, error) {
	if format != reasoningFormatOpenRouter {
		return nil, nil
	}
	providerContext, present := content.ProviderContext.Get()
	if !present || len(providerContext.Payload) == 0 || !providerContextCompatible(providerContext.Source, target) {
		return nil, nil
	}
	var details []jsontext.Value
	if err := json.Unmarshal(providerContext.Payload, &details); err != nil {
		return nil, fmt.Errorf("decode OpenRouter reasoning_details context: %w", err)
	}
	for index, detail := range details {
		var object map[string]jsontext.Value
		if err := json.Unmarshal(detail, &object); err != nil || object == nil {
			return nil, fmt.Errorf("decode OpenRouter reasoning_details context: detail %d is not an object", index)
		}
	}
	return details, nil
}

// providerContextCompatible applies exact-model and additive compatibility-key replay rules.
func providerContextCompatible(source, target model.ProviderContextSource) bool {
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
