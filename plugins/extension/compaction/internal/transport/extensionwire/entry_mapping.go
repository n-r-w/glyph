package extensionwire

import (
	"slices"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// newTextBlock creates one textual content block.
func newTextBlock(kind, text string) compaction.Block {
	return compaction.Block{Kind: kind, Text: text, MediaType: "", Data: nil, CallID: "", Name: "", Arguments: nil}
}

// newImageBlock creates an image content block.
func newImageBlock(mediaType string, data []byte) compaction.Block {
	return compaction.Block{
		Kind:      compaction.Image,
		Text:      "",
		MediaType: mediaType,
		Data:      slices.Clone(data),
		CallID:    "",
		Name:      "",
		Arguments: nil,
	}
}

// DecodeHistory maps projected provider-neutral history into policy data.
func DecodeHistory(values []*pb.CompactionHistoryRecord) []compaction.Entry {
	history := make([]compaction.Entry, 0, len(values))
	for _, value := range values {
		entry := compaction.Entry{
			ID:              value.GetSourceEntryId(),
			Kind:            "",
			Blocks:          nil,
			CallID:          "",
			Tool:            "",
			Failed:          false,
			SourcePosition:  value.GetSourcePosition(),
			SourceKind:      decodeHistorySourceKind(value.GetSourceKind()),
			PreviousSummary: value.GetPreviousSummary(),
			PrivateBytes:    value.GetPrivatePayloadBytes(),
			Tokens:          value.GetEstimatedTokens(),
		}
		switch {
		case value.GetUser() != nil:
			entry.Kind = compaction.User
			for _, block := range value.GetUser().GetContent() {
				if image := block.GetImage(); image != nil {
					entry.Blocks = append(entry.Blocks, newImageBlock(image.GetMediaType(), image.GetData()))
				} else {
					entry.Blocks = append(entry.Blocks, newTextBlock(compaction.Text, block.GetText()))
				}
			}
		case value.GetModel() != nil:
			entry.Kind = compaction.Assistant
			entry.Blocks = decodeModelBlocks(value.GetModel().GetContent())
		case value.GetToolResult() != nil:
			result := value.GetToolResult()
			entry.Kind = compaction.ToolResult
			entry.CallID = result.GetCallId()
			entry.Tool = result.GetToolName()
			entry.Failed = result.GetIsError()
			for _, block := range result.GetContents() {
				if image := block.GetImage(); image != nil {
					entry.Blocks = append(entry.Blocks, newImageBlock(image.GetMediaType(), image.GetData()))
				} else {
					entry.Blocks = append(entry.Blocks, newTextBlock(compaction.Text, block.GetText()))
				}
			}
		}
		history = append(history, entry)
	}
	return history
}

// encodeHistory maps prepared content and its source correspondence for composition.
func encodeHistory(entries []compaction.Entry) []*pb.CompactionHistoryRecord {
	values := make([]*pb.CompactionHistoryRecord, 0, len(entries))
	for index := range entries {
		entry := &entries[index]
		value := pb.CompactionHistoryRecord_builder{
			User: nil, Model: nil, ToolResult: nil,
			PrivatePayloadBytes: new(entry.PrivateBytes),
			SourceEntryId:       new(entry.ID),
			SourcePosition:      new(entry.SourcePosition),
			SourceKind:          new(encodeHistorySourceKind(entry.SourceKind)),
			PreviousSummary:     new(entry.PreviousSummary),
			EstimatedTokens:     new(entry.Tokens),
		}.Build()
		switch entry.Kind {
		case compaction.User:
			blocks := make([]*pb.SessionTreeUserContent, 0, len(entry.Blocks))
			for position := range entry.Blocks {
				block := &entry.Blocks[position]
				content := new(pb.SessionTreeUserContent)
				if block.Kind == compaction.Image {
					content.SetImage(
						pb.SessionTreeImage_builder{
							MediaType: new(block.MediaType),
							Data:      slices.Clone(block.Data),
						}.Build(),
					)
				} else {
					content.SetText(block.Text)
				}
				blocks = append(blocks, content)
			}
			value.SetUser(pb.SessionTreeUserMessage_builder{Content: blocks}.Build())
		case compaction.Assistant:
			value.SetModel(pb.SessionTreeModelResponse_builder{Content: encodeModelBlocks(entry.Blocks)}.Build())
		case compaction.ToolResult:
			blocks := make([]*pb.ToolResultContent, 0, len(entry.Blocks))
			for position := range entry.Blocks {
				block := &entry.Blocks[position]
				content := new(pb.ToolResultContent)
				if block.Kind == compaction.Image {
					content.SetImage(
						pb.ToolResultImage_builder{
							MediaType: new(block.MediaType),
							Data:      slices.Clone(block.Data),
						}.Build(),
					)
				} else {
					content.SetText(block.Text)
				}
				blocks = append(blocks, content)
			}
			value.SetToolResult(
				pb.SessionTreeToolResult_builder{
					CallId:   new(entry.CallID),
					ToolName: new(entry.Tool),
					IsError:  new(entry.Failed),
					Contents: blocks,
				}.Build(),
			)
		}
		values = append(values, value)
	}
	return values
}

// decodeModelBlocks maps model text and tool requests into policy data.
func decodeModelBlocks(values []*pb.SessionTreeModelContent) []compaction.Block {
	blocks := make([]compaction.Block, 0, len(values))
	for _, block := range values {
		mapped := newTextBlock("", block.GetText())
		switch block.GetKind() {
		case pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED:
		case pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT:
			mapped.Kind = compaction.Text
		case pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL:
			mapped.Kind = compaction.Refusal
		case pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING:
			mapped.Kind = compaction.Thought
		case pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL:
			mapped.Kind = compaction.Call
			mapped.CallID = block.GetToolCall().GetId()
			mapped.Name = block.GetToolCall().GetName()
			mapped.Arguments = slices.Clone(block.GetToolCall().GetArgumentsJson())
		}
		blocks = append(blocks, mapped)
	}
	return blocks
}

// encodeModelBlocks maps model text and tool requests.
func encodeModelBlocks(values []compaction.Block) []*pb.SessionTreeModelContent {
	blocks := make([]*pb.SessionTreeModelContent, 0, len(values))
	for index := range values {
		block := &values[index]
		content := pb.SessionTreeModelContent_builder{Kind: nil, Text: nil, ToolCall: nil}.Build()
		switch block.Kind {
		case compaction.Text:
			content.SetKind(pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT)
			content.SetText(block.Text)
		case compaction.Refusal:
			content.SetKind(pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL)
			content.SetText(block.Text)
		case compaction.Thought:
			content.SetKind(pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING)
			content.SetText(block.Text)
		case compaction.Call:
			content.SetKind(pb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL)
			content.SetToolCall(
				pb.SessionTreeToolCall_builder{
					Id:            new(block.CallID),
					Name:          new(block.Name),
					ArgumentsJson: slices.Clone(block.Arguments),
				}.Build(),
			)
		}
		blocks = append(blocks, content)
	}
	return blocks
}
