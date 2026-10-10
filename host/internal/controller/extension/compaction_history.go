// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"bytes"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// CountCompactionPrivateBytes supplies raw replay size while retaining private payloads with Host.
func CountCompactionPrivateBytes(response model.Response) int64 {
	// total contains the combined private replay payload size in bytes.
	var total int64
	for index := range response.Content {
		// content identifies the model part whose private payload size is counted.
		content := &response.Content[index]
		// private contains optional provider replay data; present indicates its availability.
		if private, present := content.ProviderContext.Get(); present {
			total += int64(len(private.Payload))
		}
	}
	return total
}

// MapCompactionHistory encodes prepared history for policy input and candidate projection.
func MapCompactionHistory(history []agent.HistoryEntry) ([]*extensionpb.CompactionHistoryRecord, error) {
	// records contains the ordered public history representation.
	records := make([]*extensionpb.CompactionHistoryRecord, 0, len(history))
	for index := range history {
		// entry identifies the prepared history record being encoded.
		entry := &history[index]
		// builder contains one public history record and its private payload size.
		builder := extensionpb.CompactionHistoryRecord_builder{
			User:                nil,
			Model:               nil,
			ToolResult:          nil,
			PrivatePayloadBytes: new(int64(0)),
		}
		switch entry.Kind {
		case agent.HistoryEntryUser:
			builder.User = mapCompactionUser(entry.User.OrEmpty())
		case agent.HistoryEntryModel:
			// response contains the prepared model response.
			response := entry.Model.OrEmpty()
			// mapped and err contain public model content and its encoding failure.
			mapped, err := mapCompactionModelResponse(response)
			if err != nil {
				return nil, err
			}
			builder.Model = mapped
			builder.PrivatePayloadBytes = new(CountCompactionPrivateBytes(response))
		case agent.HistoryEntryToolResult:
			// result contains the prepared tool result.
			result := entry.ToolResult.OrEmpty()
			// contents contains the ordered public tool-result content.
			contents := make([]*extensionpb.ToolResultContent, 0, len(result.Contents))
			// content contains one tool-result part for public encoding.
			for _, content := range result.Contents {
				// mapped contains the public tool-result part.
				mapped := new(extensionpb.ToolResultContent)
				switch content.Kind {
				case tool.ResultContentText:
					mapped.SetText(content.Text.OrEmpty())
				case tool.ResultContentImage:
					// image contains the tool-result image payload.
					image := content.Image.OrEmpty()
					mapped.SetImage(
						extensionpb.ToolResultImage_builder{
							MediaType: new(image.MediaType),
							Data:      bytes.Clone(image.Data),
						}.Build(),
					)
				default:
					return nil, fmt.Errorf("unknown compaction tool content kind %d", content.Kind)
				}
				contents = append(contents, mapped)
			}
			builder.ToolResult = extensionpb.SessionTreeToolResult_builder{
				CallId: new(result.CallID), ToolName: new(result.ToolName),
				Contents: contents, IsError: new(result.IsError),
			}.Build()
		default:
			return nil, fmt.Errorf("unknown compaction history kind %d", entry.Kind)
		}
		records = append(records, builder.Build())
	}
	return records, nil
}

// mapCompactionUser encodes ordered user content for prepared history.
func mapCompactionUser(message model.Message) *extensionpb.SessionTreeUserMessage {
	// contents contains the ordered public user content.
	contents := make([]*extensionpb.SessionTreeUserContent, 0, len(message.Content))
	// content contains one prepared user-message part.
	for _, content := range message.Content {
		// mapped contains the public user-message part.
		mapped := new(extensionpb.SessionTreeUserContent)
		// text contains optional user text; present indicates its availability.
		if text, present := content.Text.Get(); present {
			mapped.SetText(text)
		}
		if content.Kind == model.InputContentImage {
			mapped.SetImage(
				extensionpb.SessionTreeImage_builder{
					MediaType: new(content.MediaType.OrEmpty()),
					Data:      bytes.Clone(content.Data.OrEmpty()),
				}.Build(),
			)
		}
		contents = append(contents, mapped)
	}
	return extensionpb.SessionTreeUserMessage_builder{Content: contents}.Build()
}

// mapCompactionModelResponse encodes public model content for prepared history.
func mapCompactionModelResponse(response model.Response) (*extensionpb.SessionTreeModelResponse, error) {
	// contents contains the ordered public model content.
	contents := make([]*extensionpb.SessionTreeModelContent, 0, len(response.Content))
	for index := range response.Content {
		// content identifies the prepared model part being encoded.
		content := &response.Content[index]
		// kind contains the public model-content discriminator.
		kind := MapCompactionContentKind(content.Kind)
		if kind == extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED {
			return nil, fmt.Errorf("unknown compaction model content kind %d", content.Kind)
		}
		// builder contains the public model part.
		builder := extensionpb.SessionTreeModelContent_builder{Kind: new(kind), Text: nil, ToolCall: nil}
		// text contains optional model text; present indicates its availability.
		if text, present := content.Text.Get(); present {
			builder.Text = new(text)
		}
		// call contains an optional tool call; present indicates its availability.
		if call, present := content.ToolCall.Get(); present {
			builder.ToolCall = extensionpb.SessionTreeToolCall_builder{
				Id:            new(call.ID),
				Name:          new(call.Name),
				ArgumentsJson: call.Arguments.Bytes(),
			}.Build()
		}
		contents = append(contents, builder.Build())
	}
	return extensionpb.SessionTreeModelResponse_builder{Content: contents}.Build(), nil
}

// MapCompactionContentKind supplies the public representation of a model content kind.
func MapCompactionContentKind(kind model.ContentKind) extensionpb.SessionTreeModelContentKind {
	switch kind {
	case model.ContentText:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT
	case model.ContentRefusal:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL
	case model.ContentReasoning:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING
	case model.ContentToolCall:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL
	default:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED
	}
}
