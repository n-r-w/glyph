package run

import (
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//nolint:misspell // This exact model-visible error is stored for skipped calls.
const skippedCallError = "Tool call skipped because the agent run was cancelled."

// cloneHistory isolates mutable provider bytes, argument maps, and slices.
func cloneHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	result := make([]agent.HistoryEntry, len(history))
	for index := range history {
		result[index] = cloneHistoryEntry(history[index])
	}
	return result
}

// cloneHistoryEntry returns one independent history value.
func cloneHistoryEntry(entry agent.HistoryEntry) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind:       entry.Kind,
		User:       cloneMessage(entry.User),
		Model:      cloneModelResponse(entry.Model),
		ToolResult: cloneToolResult(entry.ToolResult),
	}
}

// cloneToolResult isolates mutable image bytes in returned history snapshots.
func cloneToolResult(result agent.ToolResult) agent.ToolResult {
	contents := make([]tool.ResultContent, len(result.Contents))
	for index, content := range result.Contents {
		contents[index] = tool.ResultContent{
			Kind: content.Kind, Text: content.Text,
			Image: tool.ResultImage{MediaType: content.Image.MediaType, Data: append([]byte(nil), content.Image.Data...)},
		}
	}
	return agent.ToolResult{CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError}
}

// cloneMessage isolates user content and image bytes.
func cloneMessage(message model.Message) model.Message {
	content := make([]model.InputContent, len(message.Content))
	for index, item := range message.Content {
		content[index] = model.InputContent{
			Kind: item.Kind, Text: item.Text, MediaType: item.MediaType,
			Data: append([]byte(nil), item.Data...),
		}
	}
	return model.Message{Content: content}
}

// cloneModelResponse preserves ordered content while isolating mutable values.
func cloneToolPreviews(previews map[string]model.ToolCallPreview) map[string]model.ToolCallPreview {
	if previews == nil {
		return nil
	}
	cloned := make(map[string]model.ToolCallPreview, len(previews))
	for callID, preview := range previews {
		preview.Fields = clonePreviewFields(preview.Fields)
		cloned[callID] = preview
	}
	return cloned
}

func clonePreviewFields(fields []model.ToolCallPreviewField) []model.ToolCallPreviewField {
	if fields == nil {
		return nil
	}
	cloned := make([]model.ToolCallPreviewField, len(fields))
	for index, field := range fields {
		field.Value = cloneJSONValue(field.Value)
		cloned[index] = field
	}
	return cloned
}

func cloneModelResponse(response model.Response) model.Response {
	items := make([]model.Content, len(response.Content))
	for index, item := range response.Content {
		items[index] = model.Content{
			Kind:  item.Kind,
			Text:  item.Text,
			Final: item.Final,
			ProviderContext: model.ProviderContext{
				ProviderID: item.ProviderContext.ProviderID,
				Payload:    append([]byte(nil), item.ProviderContext.Payload...),
			},
			ToolCall: model.ToolCall{
				ID:        item.ToolCall.ID,
				Name:      item.ToolCall.Name,
				Arguments: cloneArguments(item.ToolCall.Arguments),
			},
		}
	}
	var responseModel *model.ID
	if response.ResponseModel != nil {
		value := *response.ResponseModel
		responseModel = &value
	}
	diagnostics := append([]model.Diagnostic(nil), response.Diagnostics...)
	return model.Response{
		Content: items, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: responseModel,
		ResponseID: response.ResponseID, Usage: response.Usage, Diagnostics: diagnostics,
	}
}

// cloneArguments recursively preserves the JSON-compatible argument tree.
func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	result := make(map[string]any, len(arguments))
	for name, value := range arguments {
		result[name] = cloneJSONValue(value)
	}
	return result
}

// cloneJSONValue copies the mutable JSON-compatible collection variants.
func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneJSONValue(item)
		}
		return result
	default:
		return value
	}
}

// projectHistory excludes failed responses and supplies temporary skipped results.
func projectHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	projected := make([]agent.HistoryEntry, 0, len(history))
	for index := 0; index < len(history); {
		entry := history[index]
		if entry.Kind != agent.HistoryEntryModel {
			projected = append(projected, cloneHistoryEntry(entry))
			index++
			continue
		}
		if entry.Model.Outcome == model.OutcomeAborted || entry.Model.Outcome == model.OutcomeFailed {
			index++
			continue
		}

		projected = append(projected, cloneHistoryEntry(entry))
		results := make(map[string]agent.ToolResult)
		next := index + 1
		for next < len(history) && history[next].Kind == agent.HistoryEntryToolResult {
			result := history[next].ToolResult
			results[result.CallID] = result
			projected = append(projected, cloneHistoryEntry(history[next]))
			next++
		}
		for _, call := range modelToolCalls(entry.Model) {
			if _, exists := results[call.ID]; exists {
				continue
			}
			projected = append(projected, agent.HistoryEntry{
				Kind:  agent.HistoryEntryToolResult,
				User:  model.TextMessage(""),
				Model: emptyModelResponse(),
				ToolResult: agent.ToolResult{
					CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(skippedCallError), IsError: true,
				},
			})
		}
		index = next
	}
	return projected
}

// modelToolCalls returns finalized calls in model-provided order.
func modelToolCalls(response model.Response) []model.ToolCall {
	calls := make([]model.ToolCall, 0)
	for _, item := range response.Content {
		if item.Kind == model.ContentToolCall {
			calls = append(calls, item.ToolCall)
		}
	}
	return calls
}
