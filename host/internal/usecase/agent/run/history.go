package run

import (
	"bytes"
	"maps"
	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//nolint:misspell // This exact model-visible error is stored for skipped calls.
const skippedCallError = "Tool call skipped because the agent run was cancelled."

// cloneHistory isolates mutable provider bytes, argument maps, and slices.
func cloneHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	cloned := slices.Clone(history)
	for index := range cloned {
		cloned[index] = cloneHistoryEntry(cloned[index])
	}
	return cloned
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
	result.Contents = slices.Clone(result.Contents)
	for index := range result.Contents {
		image, ok := result.Contents[index].Image.Get()
		if !ok {
			continue
		}
		image.Data = bytes.Clone(image.Data)
		result.Contents[index].Image = mo.Some(image)
	}
	return result
}

// cloneMessage isolates user content and image bytes.
func cloneMessage(message model.Message) model.Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = bytes.Clone(message.Content[index].Data)
	}
	return message
}

// cloneModelResponse preserves ordered content while isolating mutable values.
func cloneToolPreviews(previews map[string]model.ToolCallPreview) map[string]model.ToolCallPreview {
	if previews == nil {
		return nil
	}
	cloned := maps.Clone(previews)
	for callID, preview := range cloned {
		preview.Fields = clonePreviewFields(preview.Fields)
		cloned[callID] = preview
	}
	return cloned
}

func clonePreviewFields(fields []model.ToolCallPreviewField) []model.ToolCallPreviewField {
	if fields == nil {
		return nil
	}
	cloned := slices.Clone(fields)
	for index := range cloned {
		cloned[index].Value = cloneJSONValue(cloned[index].Value)
	}
	return cloned
}

func cloneModelResponse(response model.Response) model.Response {
	items := slices.Clone(response.Content)
	for index := range items {
		items[index].ProviderContext.Payload = bytes.Clone(items[index].ProviderContext.Payload)
		items[index].ToolCall.Arguments = cloneArguments(items[index].ToolCall.Arguments)
	}
	var responseModel *model.ID
	if response.ResponseModel != nil {
		value := *response.ResponseModel
		responseModel = &value
	}
	diagnostics := slices.Clone(response.Diagnostics)
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
	result := maps.Clone(arguments)
	for name, value := range result {
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
		result := slices.Clone(typed)
		for index, item := range result {
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
	return lo.FilterMap(response.Content, func(item model.Content, _ int) (model.ToolCall, bool) {
		return item.ToolCall, item.Kind == model.ContentToolCall
	})
}
