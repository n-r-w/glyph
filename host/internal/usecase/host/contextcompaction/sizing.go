package contextcompaction

import (
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

const (
	// recordFramingTokens is the provider-neutral allowance for one model-visible record.
	recordFramingTokens int64 = 8
	// approximateBytesPerToken is the approved average text compression ratio.
	approximateBytesPerToken int64 = 4
	// imageTokens is the fixed estimate for one image block.
	imageTokens int64 = 2048
)

// estimateRecord applies the deterministic per-record sizing policy.
func estimateRecord(byteCount, imageCount int64) int64 {
	return recordFramingTokens + (byteCount+approximateBytesPerToken-1)/approximateBytesPerToken +
		imageTokens*imageCount
}

// fallbackEstimate estimates one complete provider-neutral request without reported usage.
func fallbackEstimate(request modelexecution.ProviderRequest) (int64, error) {
	total := int64(0)
	if request.Instructions != "" {
		total += estimateRecord(int64(len(request.Instructions)), 0)
	}
	for index := range request.Tools {
		total += estimateTool(request.Tools[index])
	}
	historyTotal, err := estimateHistory(request.History)
	if err != nil {
		return 0, err
	}
	return total + historyTotal, nil
}

// estimateHistory sums independently rounded model-visible history records.
func estimateHistory(history []agent.HistoryEntry) (int64, error) {
	total := int64(0)
	for index := range history {
		estimate, err := estimateHistoryEntry(history[index])
		if err != nil {
			return 0, fmt.Errorf("estimate history entry %d: %w", index, err)
		}
		total += estimate
	}
	return total, nil
}

// estimateTool measures one provider-neutral tool definition as one record.
func estimateTool(descriptor tool.Descriptor) int64 {
	byteCount := len(descriptor.Name) + len(descriptor.Description) + len(descriptor.InputSchemaJSON)
	if sampling, present := descriptor.ConstrainedSampling.Get(); present {
		byteCount += constrainedSamplingBytes(sampling)
	}
	return estimateRecord(int64(byteCount), 0)
}

// constrainedSamplingBytes measures every declared grammar variant and its input property.
func constrainedSamplingBytes(sampling tool.ConstrainedSampling) int {
	byteCount := 0
	if grammar, present := sampling.Grammar.Get(); present {
		if lark, larkPresent := grammar.Lark.Get(); larkPresent {
			byteCount += len(lark)
		}
		if regex, regexPresent := grammar.Regex.Get(); regexPresent {
			byteCount += len(regex)
		}
	}
	if property, present := sampling.GrammarInputProperty.Get(); present {
		byteCount += len(property)
	}
	return byteCount
}

// estimateHistoryEntry measures exactly one provider-neutral history record.
func estimateHistoryEntry(entry agent.HistoryEntry) (int64, error) {
	switch entry.Kind {
	case agent.HistoryEntryUser:
		message, present := entry.User.Get()
		if !present {
			return 0, nil
		}
		return estimateUserMessage(message), nil
	case agent.HistoryEntryModel:
		response, present := entry.Model.Get()
		if !present {
			return 0, nil
		}
		return estimateModelResponse(response)
	case agent.HistoryEntryToolResult:
		result, present := entry.ToolResult.Get()
		if !present {
			return 0, nil
		}
		return estimateToolResult(result), nil
	default:
		return 0, fmt.Errorf("unsupported history entry kind %d", entry.Kind)
	}
}

// estimateUserMessage measures text bytes and image blocks without image payload bytes.
func estimateUserMessage(message model.Message) int64 {
	byteCount := 0
	imageCount := int64(0)
	for index := range message.Content {
		content := message.Content[index]
		if content.Kind == model.InputContentText {
			if text, present := content.Text.Get(); present {
				byteCount += len(text)
			}
		}
		if content.Kind == model.InputContentImage {
			imageCount++
		}
	}
	return estimateRecord(int64(byteCount), imageCount)
}

// estimateModelResponse measures visible output, tool calls, and opaque replay lengths.
func estimateModelResponse(response model.Response) (int64, error) {
	byteCount := 0
	for index := range response.Content {
		content := response.Content[index]
		if text, present := content.Text.Get(); present &&
			(content.Kind == model.ContentText || content.Kind == model.ContentReasoning ||
				content.Kind == model.ContentRefusal) {
			byteCount += len(text)
		}
		if providerContext, present := content.ProviderContext.Get(); present {
			byteCount += len(providerContext.Payload)
		}
		if call, present := content.ToolCall.Get(); present && content.Kind == model.ContentToolCall {
			byteCount += len(call.ID) + len(call.Name) + call.Arguments.Len()
		}
	}
	return estimateRecord(int64(byteCount), 0), nil
}

// estimateToolResult measures the call identifier, text results, and image count.
func estimateToolResult(result agent.ToolResult) int64 {
	byteCount := len(result.CallID)
	imageCount := int64(0)
	for index := range result.Contents {
		content := result.Contents[index]
		if content.Kind == tool.ResultContentText {
			if text, present := content.Text.Get(); present {
				byteCount += len(text)
			}
		}
		if content.Kind == tool.ResultContentImage {
			imageCount++
		}
	}
	return estimateRecord(int64(byteCount), imageCount)
}
