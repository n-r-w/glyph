// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"bytes"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// CountCompactionPrivateBytes supplies raw replay size while retaining private payloads with Host.
func CountCompactionPrivateBytes(response model.Response) int64 {
	var total int64
	for index := range response.Content {
		content := &response.Content[index]
		if private, present := content.ProviderContext.Get(); present {
			total += int64(len(private.Payload))
		}
	}
	return total
}

// MapCompactionHistory encodes prepared history for policy input and candidate projection.
func MapCompactionHistory(history []agent.HistoryEntry) ([]*extensionpb.CompactionHistoryRecord, error) {
	records := make([]*extensionpb.CompactionHistoryRecord, 0, len(history))
	for index := range history {
		entry := &history[index]
		builder := extensionpb.CompactionHistoryRecord_builder{
			SourceEntryId: nil, SourcePosition: nil, SourceKind: nil, PreviousSummary: nil, EstimatedTokens: nil,
			User:                nil,
			Model:               nil,
			ToolResult:          nil,
			PrivatePayloadBytes: new(int64(0)),
		}
		switch entry.Kind {
		case agent.HistoryEntryUser:
			builder.User = mapCompactionUser(entry.User.OrEmpty())
		case agent.HistoryEntryModel:
			response := entry.Model.OrEmpty()
			mapped, err := mapCompactionModelResponse(response)
			if err != nil {
				return nil, err
			}
			builder.Model = mapped
			builder.PrivatePayloadBytes = new(CountCompactionPrivateBytes(response))
		case agent.HistoryEntryToolResult:
			result := entry.ToolResult.OrEmpty()
			mapped, err := mapCompactionToolResult(result)
			if err != nil {
				return nil, err
			}
			builder.ToolResult = mapped
		default:
			return nil, fmt.Errorf("unknown compaction history kind %d", entry.Kind)
		}
		records = append(records, builder.Build())
	}
	return records, nil
}

// mapCompactionUser encodes ordered user content for prepared history.
func mapCompactionUser(message model.Message) *extensionpb.SessionTreeUserMessage {
	contents := make([]*extensionpb.SessionTreeUserContent, 0, len(message.Content))
	for _, content := range message.Content {
		mapped := new(extensionpb.SessionTreeUserContent)
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
	contents := make([]*extensionpb.SessionTreeModelContent, 0, len(response.Content))
	for index := range response.Content {
		content := &response.Content[index]
		kind := MapCompactionContentKind(content.Kind)
		if kind == extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED {
			return nil, fmt.Errorf("unknown compaction model content kind %d", content.Kind)
		}
		builder := extensionpb.SessionTreeModelContent_builder{Kind: new(kind), Text: nil, ToolCall: nil}
		if text, present := content.Text.Get(); present {
			builder.Text = new(text)
		}
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

// mapCompactionToolResult projects ordered model-visible tool output.
func mapCompactionToolResult(result agent.ToolResult) (*extensionpb.SessionTreeToolResult, error) {
	contents := make([]*extensionpb.ToolResultContent, 0, len(result.Contents))
	for _, content := range result.Contents {
		mapped := new(extensionpb.ToolResultContent)
		switch content.Kind {
		case tool.ResultContentText:
			mapped.SetText(content.Text.OrEmpty())
		case tool.ResultContentImage:
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
	return extensionpb.SessionTreeToolResult_builder{
		CallId: new(result.CallID), ToolName: new(result.ToolName),
		Contents: contents, IsError: new(result.IsError),
	}.Build(), nil
}

// MapPreparedCompactionHistory supplies prepared content with its original branch correspondence.
func MapPreparedCompactionHistory(history session.PreparedHistory) ([]*extensionpb.CompactionHistoryRecord, error) {
	wires, err := MapCompactionHistory(history.Content())
	if err != nil {
		return nil, err
	}
	for index := range history {
		kind, sourceErr := mapHistorySourceKind(history[index].SourceKind)
		if sourceErr != nil {
			return nil, sourceErr
		}
		wires[index].SetSourceKind(kind)
		wires[index].SetSourceEntryId(history[index].SourceID)
		wires[index].SetSourcePosition(history[index].SourcePosition)
		wires[index].SetPreviousSummary(history[index].PreviousSummary)
	}
	return wires, nil
}
