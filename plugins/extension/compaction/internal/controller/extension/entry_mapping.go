package extension

import (
	"slices"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// decodeEntry maps an original branch entry into policy data.
func decodeEntry(value *pb.SessionTreeEntry) compaction.Entry {
	entry := compaction.Entry{
		ID:           value.GetId(),
		Kind:         "",
		Blocks:       nil,
		CallID:       "",
		Tool:         "",
		Failed:       false,
		Extension:    "",
		EntryType:    "",
		Visibility:   compaction.VisibilityUnspecified,
		Marker:       mo.None[compaction.Result](),
		PrivateBytes: 0,
		Tokens:       0,
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
		tool := value.GetToolResult()
		entry.Kind = compaction.ToolResult
		entry.CallID = tool.GetCallId()
		entry.Tool = tool.GetToolName()
		entry.Failed = tool.GetIsError()
		for _, block := range tool.GetContents() {
			if image := block.GetImage(); image != nil {
				entry.Blocks = append(entry.Blocks, newImageBlock(image.GetMediaType(), image.GetData()))
			} else {
				entry.Blocks = append(entry.Blocks, newTextBlock(compaction.Text, block.GetText()))
			}
		}
	case value.GetBranchSummary() != nil:
		entry.Kind = compaction.BranchSummary
		entry.Blocks = []compaction.Block{newTextBlock(compaction.Text, value.GetBranchSummary().GetSummary())}
	case value.GetExtensionMessage() != nil:
		message := value.GetExtensionMessage()
		entry.Kind = compaction.ExtensionMessage
		entry.Extension = message.GetExtensionId()
		entry.EntryType = message.GetEntryType()
		entry.Visibility = decodeVisibility(message.GetVisibility())
		entry.Blocks = []compaction.Block{newTextBlock(compaction.Text, message.GetText())}
	case value.GetExtension() != nil:
		entry.Kind = compaction.ExtensionEntry
		entry.Extension = value.GetExtension().GetExtensionId()
		entry.EntryType = value.GetExtension().GetEntryType()
	case value.GetCompaction() != nil:
		entry.Kind = compaction.Marker
		entry.Marker = decodeMarker(value.GetCompaction())
	}
	return entry
}

// encodeEntry maps one original entry back to the participant contract.
func encodeEntry(entry compaction.Entry) *pb.SessionTreeEntry {
	value := new(pb.SessionTreeEntry)
	value.SetId(entry.ID)
	switch entry.Kind {
	case compaction.User:
		blocks := make([]*pb.SessionTreeUserContent, 0, len(entry.Blocks))
		for index := range entry.Blocks {
			block := &entry.Blocks[index]
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
		for index := range entry.Blocks {
			block := &entry.Blocks[index]
			content := new(pb.ToolResultContent)
			if block.Kind == compaction.Image {
				content.SetImage(
					pb.ToolResultImage_builder{MediaType: new(block.MediaType), Data: slices.Clone(block.Data)}.Build(),
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
				Contents: blocks,
				IsError:  new(entry.Failed),
			}.Build(),
		)
	case compaction.BranchSummary:
		value.SetBranchSummary(pb.SessionTreeBranchSummary_builder{Summary: new(entry.Blocks[0].Text)}.Build())
	case compaction.ExtensionMessage:
		value.SetExtensionMessage(
			pb.SessionTreeExtensionMessage_builder{
				ExtensionId: new(entry.Extension),
				EntryType:   new(entry.EntryType),
				Text:        new(entry.Blocks[0].Text),
				Visibility:  new(encodeVisibility(entry.Visibility)),
			}.Build(),
		)
	case compaction.ExtensionEntry:
		value.SetExtension(
			pb.SessionTreeExtensionEntry_builder{
				ExtensionId: new(entry.Extension),
				EntryType:   new(entry.EntryType),
			}.Build(),
		)
	case compaction.Marker:
		if marker, present := entry.Marker.Get(); present {
			value.SetCompaction(encodeMarker(marker))
		}
	}
	return value
}

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
		entry := new(pb.SessionTreeEntry)
		switch {
		case value.GetUser() != nil:
			entry.SetUser(value.GetUser())
		case value.GetModel() != nil:
			entry.SetModel(value.GetModel())
		case value.GetToolResult() != nil:
			entry.SetToolResult(value.GetToolResult())
		}
		mapped := decodeEntry(entry)
		mapped.PrivateBytes = value.GetPrivatePayloadBytes()
		history = append(history, mapped)
	}
	return history
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
