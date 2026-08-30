package sessiontree

import (
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

const (
	// branchSummaryBlockSeparator separates ordered serialized content blocks.
	branchSummaryBlockSeparator = "\n\n"
	// branchSummaryDynamicLinePrefix distinguishes source lines from serializer labels.
	branchSummaryDynamicLinePrefix = "| "
	// branchSummaryUserLabel identifies user text.
	branchSummaryUserLabel = "[User]"
	// branchSummaryReasoningLabel identifies model-visible reasoning.
	branchSummaryReasoningLabel = "[Assistant reasoning]"
	// branchSummaryAssistantLabel identifies assistant text.
	branchSummaryAssistantLabel = "[Assistant]"
	// branchSummaryRefusalLabel identifies assistant refusal text.
	branchSummaryRefusalLabel = "[Assistant refusal]"
	// branchSummaryToolCallLabel identifies one assistant tool call.
	branchSummaryToolCallLabel = "[Assistant tool call]"
	// branchSummaryToolResultLabel identifies one tool result.
	branchSummaryToolResultLabel = "[Tool result]"
	// branchSummaryPreviousSummaryLabel identifies one prior summary.
	branchSummaryPreviousSummaryLabel = "[Previous summary]"
	// branchSummaryCallIDLabel identifies a tool call identifier field.
	branchSummaryCallIDLabel = "Call ID"
	// branchSummaryToolNameLabel identifies a tool name field.
	branchSummaryToolNameLabel = "Tool name"
	// branchSummaryArgumentsLabel identifies deterministic tool arguments.
	branchSummaryArgumentsLabel = "Arguments"
	// branchSummaryErrorLabel identifies a tool result error state.
	branchSummaryErrorLabel = "Error"
	// branchSummaryContentLabel identifies one ordered tool result text block.
	branchSummaryContentLabel = "Content"
)

// branchSummaryField is one optional serializer label and its dynamic source value.
type branchSummaryField struct {
	// label identifies the dynamic value and remains outside dynamic line framing.
	label string
	// value contains source data that requires escaping and line framing.
	value string
}

// serializeBranchSummaryConversation serializes approved model-visible content in entry and block order.
func serializeBranchSummaryConversation(entries []session.Entry) (string, error) {
	var serialized strings.Builder
	hasBlock := false
	for entryIndex := range entries {
		entry := &entries[entryIndex]
		if user, present := entry.User.Get(); present {
			writeBranchSummaryUser(&serialized, &hasBlock, user)
		}
		if response, present := entry.Model.Get(); present {
			if err := writeBranchSummaryModel(&serialized, &hasBlock, response); err != nil {
				return "", err
			}
		}
		if result, present := entry.ToolResult.Get(); present {
			writeBranchSummaryToolResult(&serialized, &hasBlock, result)
		}
		if summary, present := entry.BranchSummary.Get(); present {
			writeBranchSummaryBlock(&serialized, &hasBlock, branchSummaryPreviousSummaryLabel, []branchSummaryField{{
				label: "", value: summary.Summary,
			}})
		}
	}
	return serialized.String(), nil
}

// writeBranchSummaryUser writes ordered user text blocks and excludes non-text input.
func writeBranchSummaryUser(serialized *strings.Builder, hasBlock *bool, user model.Message) {
	for contentIndex := range user.Content {
		content := &user.Content[contentIndex]
		if content.Kind == model.InputContentText {
			if text, present := content.Text.Get(); present {
				writeBranchSummaryBlock(serialized, hasBlock, branchSummaryUserLabel, []branchSummaryField{{
					label: "", value: text,
				}})
			}
		}
	}
}

// writeBranchSummaryModel writes ordered model-visible response blocks.
func writeBranchSummaryModel(
	serialized *strings.Builder,
	hasBlock *bool,
	response model.Response,
) error {
	for contentIndex := range response.Content {
		content := &response.Content[contentIndex]
		switch content.Kind {
		case model.ContentReasoning:
			writeBranchSummaryTextContent(serialized, hasBlock, branchSummaryReasoningLabel, content)
		case model.ContentText:
			writeBranchSummaryTextContent(serialized, hasBlock, branchSummaryAssistantLabel, content)
		case model.ContentRefusal:
			writeBranchSummaryTextContent(serialized, hasBlock, branchSummaryRefusalLabel, content)
		case model.ContentToolCall:
			if err := writeBranchSummaryToolCall(serialized, hasBlock, content); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeBranchSummaryToolCall writes one present tool call with deterministic arguments.
func writeBranchSummaryToolCall(
	serialized *strings.Builder,
	hasBlock *bool,
	content *model.Content,
) error {
	call, present := content.ToolCall.Get()
	if !present {
		return nil
	}
	arguments, err := json.Marshal(call.Arguments, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("encode tool call %q arguments: %w", call.ID, err)
	}
	writeBranchSummaryBlock(serialized, hasBlock, branchSummaryToolCallLabel, []branchSummaryField{
		{label: branchSummaryCallIDLabel, value: call.ID},
		{label: branchSummaryToolNameLabel, value: call.Name},
		{label: branchSummaryArgumentsLabel, value: string(arguments)},
	})
	return nil
}

// writeBranchSummaryToolResult writes tool metadata and ordered text result blocks.
func writeBranchSummaryToolResult(
	serialized *strings.Builder,
	hasBlock *bool,
	result session.ToolResult,
) {
	fields := []branchSummaryField{
		{label: branchSummaryCallIDLabel, value: result.CallID},
		{label: branchSummaryToolNameLabel, value: result.ToolName},
		{label: branchSummaryErrorLabel, value: strconv.FormatBool(result.IsError)},
	}
	for contentIndex := range result.Contents {
		content := &result.Contents[contentIndex]
		if content.Kind == tool.ResultContentText {
			if text, present := content.Text.Get(); present {
				fields = append(fields, branchSummaryField{label: branchSummaryContentLabel, value: text})
			}
		}
	}
	writeBranchSummaryBlock(serialized, hasBlock, branchSummaryToolResultLabel, fields)
}

// writeBranchSummaryTextContent writes one present model-visible text value.
func writeBranchSummaryTextContent(
	serialized *strings.Builder,
	hasBlock *bool,
	label string,
	content *model.Content,
) {
	if text, present := content.Text.Get(); present {
		writeBranchSummaryBlock(serialized, hasBlock, label, []branchSummaryField{{label: "", value: text}})
	}
}

// writeBranchSummaryBlock writes one labeled block with escaped and line-framed dynamic fields.
func writeBranchSummaryBlock(
	serialized *strings.Builder,
	hasBlock *bool,
	label string,
	fields []branchSummaryField,
) {
	if *hasBlock {
		serialized.WriteString(branchSummaryBlockSeparator)
	}
	*hasBlock = true
	serialized.WriteString(label)
	for fieldIndex := range fields {
		field := &fields[fieldIndex]
		serialized.WriteByte('\n')
		if field.label != "" {
			serialized.WriteString(field.label)
			serialized.WriteByte('\n')
		}
		writeBranchSummaryDynamicValue(serialized, field.value)
	}
}

// writeBranchSummaryDynamicValue XML-text-escapes one source value and prefixes every resulting line.
func writeBranchSummaryDynamicValue(serialized *strings.Builder, value string) {
	lines := strings.Split(escapeXMLText(value), "\n")
	for lineIndex := range lines {
		if lineIndex > 0 {
			serialized.WriteByte('\n')
		}
		serialized.WriteString(branchSummaryDynamicLinePrefix)
		serialized.WriteString(lines[lineIndex])
	}
}

// escapeXMLText escapes dynamic text for insertion inside pseudo-XML element content.
func escapeXMLText(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}
