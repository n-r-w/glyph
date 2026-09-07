package presentation

import (
	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo/option"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// decodeReasoningCapabilities constructs application-owned ReasoningCapabilities data from validated input.
func decodeReasoningCapabilities(input plugininput.ReasoningCapabilities) ReasoningCapabilities {
	return ReasoningCapabilities{
		Supported: input.Supported,
		Choices: lo.Map(
			input.Choices,
			func(value plugininput.ReasoningChoice, _ int) ReasoningChoice { return ReasoningChoice(value) },
		),
		Default: ReasoningChoice(input.Default),
	}
}

// decodeConfiguredModel constructs application-owned ConfiguredModel data from validated input.
func decodeConfiguredModel(input plugininput.ConfiguredModel) ConfiguredModel {
	return ConfiguredModel{
		ProviderID: input.ProviderID,
		ModelID:    input.ModelID,
		Reasoning:  decodeReasoningCapabilities(input.Reasoning),
	}
}

// decodeModelSelection constructs application-owned ModelSelection data from validated input.
func decodeModelSelection(input plugininput.ModelSelection) ModelSelection {
	return ModelSelection{
		ProviderID:      input.ProviderID,
		ModelID:         input.ModelID,
		ReasoningChoice: ReasoningChoice(input.ReasoningChoice),
	}
}

// decodeModelResponseContent constructs application-owned ModelResponseContent data from validated input.
func decodeModelResponseContent(input plugininput.ModelResponseContent) ModelResponseContent {
	return ModelResponseContent{
		Kind: ModelContentKind(input.Kind),
		Text: input.Text,
	}
}

// decodeContent constructs application-owned Content data from validated input.
func decodeContent(input plugininput.Content) Content {
	return Content{
		Text:      input.Text,
		MediaType: input.MediaType,
		Data:      input.Data.MapValue(slices.Clone[[]byte]),
	}
}

// decodeSessionInfo constructs application-owned SessionInfo data from validated input.
func decodeSessionInfo(input plugininput.SessionInfo) SessionInfo {
	return SessionInfo{
		ID:               input.ID,
		Name:             input.Name,
		NamePresent:      input.NamePresent,
		WorkingDirectory: input.WorkingDirectory,
		StoragePath:      input.StoragePath,
		StoragePresent:   input.StoragePresent,
		CreatedAt:        input.CreatedAt,
		UpdatedAt:        input.UpdatedAt,
	}
}

// decodeTokenUsage constructs application-owned TokenUsage data from validated input.
func decodeTokenUsage(input plugininput.TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens:      input.InputTokens,
		OutputTokens:     input.OutputTokens,
		CacheReadTokens:  input.CacheReadTokens,
		CacheWriteTokens: input.CacheWriteTokens,
		ReasoningTokens:  input.ReasoningTokens,
		TotalTokens:      input.TotalTokens,
	}
}

// decodeEstimatedCost constructs application-owned EstimatedCost data from validated input.
func decodeEstimatedCost(input plugininput.EstimatedCost) EstimatedCost {
	return EstimatedCost{
		Input:      input.Input,
		Output:     input.Output,
		CacheRead:  input.CacheRead,
		CacheWrite: input.CacheWrite,
		Total:      input.Total,
	}
}

// decodeProviderModelCost constructs application-owned ProviderModelCost data from validated input.
func decodeProviderModelCost(input plugininput.ProviderModelCost) ProviderModelCost {
	return ProviderModelCost{
		ProviderID: input.ProviderID,
		ModelID:    input.ModelID,
		EstimatedCost: option.Map(
			decodeEstimatedCost,
		)(
			input.EstimatedCost,
		),
	}
}

// decodeSessionStatistics constructs application-owned SessionStatistics data from validated input.
func decodeSessionStatistics(input plugininput.SessionStatistics) SessionStatistics {
	return SessionStatistics{
		UserMessages:   input.UserMessages,
		ModelResponses: input.ModelResponses,
		ToolCalls:      input.ToolCalls,
		ToolResults:    input.ToolResults,
		TotalMessages:  input.TotalMessages,
		TokenUsage: option.Map(
			decodeTokenUsage,
		)(
			input.TokenUsage,
		),
		EstimatedCost: option.Map(
			decodeEstimatedCost,
		)(
			input.EstimatedCost,
		),
		CostBreakdown: lo.Map(input.CostBreakdown, func(value plugininput.ProviderModelCost, _ int) ProviderModelCost {
			return decodeProviderModelCost(value)
		}),
	}
}

// decodeSessionSummary constructs application-owned SessionSummary data from validated input.
func decodeSessionSummary(input plugininput.SessionSummary) SessionSummary {
	return SessionSummary{
		Info:          decodeSessionInfo(input.Info),
		FirstUserText: input.FirstUserText,
		TextPresent:   input.TextPresent,
		TotalMessages: input.TotalMessages,
	}
}

// decodeTranscript constructs application-owned Line data from validated input.
func decodeTranscript(input plugininput.Transcript) Line {
	return Line{
		Kind:     LineKind(input.Kind),
		ToolName: input.ToolName,
		Status:   input.Status,
		Text:     input.Text,
		Contents: option.Map(func(value []plugininput.Content) []Content {
			return lo.Map(value, func(value plugininput.Content, _ int) Content { return decodeContent(value) })
		})(input.Contents),
	}
}

// decodeToolCallField constructs application-owned ToolCallField data from validated input.
func decodeToolCallField(input plugininput.ToolCallField) ToolCallField {
	return ToolCallField{
		Name:   input.Name,
		Value:  input.Value.MapValue(cloneJSONValue),
		Prefix: input.Prefix,
	}
}

// decodeToolCallState constructs application-owned ToolCallState data from validated input.
func decodeToolCallState(input plugininput.ToolCallState) ToolCallState {
	return ToolCallState{
		CallID:      input.CallID,
		Name:        input.Name,
		Position:    input.Position,
		Provisional: input.Provisional,
		Fields: lo.Map(
			input.Fields,
			func(value plugininput.ToolCallField, _ int) ToolCallField { return decodeToolCallField(value) },
		),
		Arguments: cloneJSONMap(input.Arguments),
	}
}

// decodeOperationIssue constructs application-owned OperationIssue data from validated input.
func decodeOperationIssue(input plugininput.OperationIssue) OperationIssue {
	return OperationIssue{
		Code:        input.Code,
		ExtensionID: input.ExtensionID,
		HandlerID:   input.HandlerID,
		Message:     input.Message,
	}
}

// decodeExtensionMessage constructs application-owned ExtensionMessage data from validated input.
func decodeExtensionMessage(input plugininput.ExtensionMessage) ExtensionMessage {
	return ExtensionMessage{
		ExtensionID: input.ExtensionID,
		EntryType:   input.EntryType,
		Text:        input.Text,
		Visibility:  ClientVisibility(input.Visibility),
	}
}

// decodeTreeEntry constructs application-owned TreeEntry data from validated input.
func decodeTreeEntry(input plugininput.TreeEntry) TreeEntry {
	return TreeEntry{
		ID:        input.ID,
		ParentID:  input.ParentID,
		CreatedAt: input.CreatedAt,
		Label:     input.Label,
		Kind:      TreeEntryKind(input.Kind),
		ExtensionMessage: option.Map(
			decodeExtensionMessage,
		)(
			input.ExtensionMessage,
		),
		Text: input.Text,
	}
}

// decodeSessionTree constructs application-owned SessionTree data from validated input.
func decodeSessionTree(input plugininput.SessionTree) SessionTree {
	return SessionTree{
		Entries: lo.Map(
			input.Entries,
			func(value plugininput.TreeEntry, _ int) TreeEntry { return decodeTreeEntry(value) },
		),
		ActiveLeafID: input.ActiveLeafID,
	}
}
