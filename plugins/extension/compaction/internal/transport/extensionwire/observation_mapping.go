package extensionwire

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// DecodeBinding identifies the extension context for conversation observations.
func DecodeBinding(value *pb.ExtensionContext) compaction.Binding {
	return compaction.Binding{
		Context: value.GetContextId(),
		Runtime: value.GetRuntimeInstanceId(),
		Session: value.GetSessionId(),
	}
}

// DecodeObservation maps actual conversation input and completed response content.
func DecodeObservation(value *pb.CompletedConversationObservation, binding compaction.Binding) compaction.Observation {
	input := compaction.Input{
		Binding:  binding,
		Entries:  nil,
		History:  DecodeHistory(value.GetHistory()),
		Previous: mo.None[compaction.Result](),
		Model: decodeModel(
			value.GetModel(),
		),
		Reasoning:    value.GetReasoningChoice(),
		Instructions: value.GetInstructions(),
		ToolText: decodeToolText(
			value.GetTools(),
		),
		ToolShape:  decodeToolShape(value.GetTools()),
		Guidance:   "",
		Purpose:    compaction.PurposeUnspecified,
		Requests:   nil,
		Generators: nil,
		Results:    nil,
	}
	response := compaction.Entry{
		ID:           "",
		Kind:         compaction.Assistant,
		Blocks:       nil,
		CallID:       "",
		Tool:         "",
		Failed:       false,
		Extension:    "",
		EntryType:    "",
		Visibility:   compaction.VisibilityUnspecified,
		Marker:       mo.None[compaction.Result](),
		PrivateBytes: value.GetResponsePrivatePayloadBytes(),
		Tokens:       0,
	}
	for _, content := range value.GetResponse().GetContent() {
		block := compaction.Block{Kind: "", Text: "", MediaType: "", Data: nil, CallID: "", Name: "", Arguments: nil}
		switch {
		case content.GetText() != nil:
			block.Kind, block.Text = compaction.Text, content.GetText().GetText()
		case content.GetRefusal() != nil:
			block.Kind, block.Text = compaction.Refusal, content.GetRefusal().GetText()
		case content.GetReasoning() != nil:
			block.Kind, block.Text = compaction.Thought, content.GetReasoning().GetText()
		case content.GetToolCall() != nil:
			call := content.GetToolCall()
			block.Kind, block.CallID, block.Name, block.Arguments = compaction.Call, call.GetId(), call.GetName(), bytes.Clone(
				call.GetArgumentsJson(),
			)
		}
		response.Blocks = append(response.Blocks, block)
	}
	return compaction.Observation{Input: input, Response: response, Usage: DecodeUsage(value.GetResponse().GetUsage())}
}

// decodeToolShape preserves tool-definition boundaries for continuation matching.
func decodeToolShape(values []*pb.CompactionToolDefinition) string {
	var shape strings.Builder
	for _, value := range values {
		descriptor := value.GetDescriptor()
		constraint := descriptor.GetConstrainedSampling()
		for _, field := range []string{
			descriptor.GetName(), descriptor.GetDescription(),
			string(descriptor.GetInputSchemaJson()), value.GetGrammarInputProperty(),
			strconv.FormatBool(constraint.GetJsonSchema() != nil), constraint.GetJsonSchema().GetStrictness().String(),
			strconv.FormatBool(constraint.GetGrammar() != nil),
			constraint.GetGrammar().GetLark(), constraint.GetGrammar().GetRegex(),
		} {
			shape.WriteString(strconv.Quote(field))
		}
	}
	return shape.String()
}
