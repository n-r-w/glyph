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
		User:       entry.User.MapValue(cloneMessage),
		Model:      entry.Model.MapValue(cloneModelResponse),
		ToolResult: entry.ToolResult.MapValue(cloneToolResult),
	}
}

// cloneToolResult isolates mutable image bytes in returned history snapshots.
func cloneToolResult(result agent.ToolResult) agent.ToolResult {
	result.Contents = slices.Clone(result.Contents)
	for index := range result.Contents {
		result.Contents[index].Image = result.Contents[index].Image.MapValue(func(image tool.ResultImage) tool.ResultImage {
			image.Data = bytes.Clone(image.Data)
			return image
		})
	}
	return result
}

// cloneMessage isolates user content and image bytes.
func cloneMessage(message model.Message) model.Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = message.Content[index].Data.MapValue(bytes.Clone)
	}
	return message
}

// cloneToolPreviews isolates mutable preview fields.
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
		cloned[index].Value = cloned[index].Value.MapValue(cloneJSONValue)
	}
	return cloned
}

// cloneModelResponse preserves ordered content while isolating mutable values.
func cloneModelResponse(response model.Response) model.Response {
	items := slices.Clone(response.Content)
	for index := range items {
		if context, ok := items[index].ProviderContext.Get(); ok {
			context.Payload = bytes.Clone(context.Payload)
			items[index].ProviderContext = mo.Some(context)
		}
		if call, ok := items[index].ToolCall.Get(); ok {
			call.Arguments = cloneArguments(call.Arguments)
			items[index].ToolCall = mo.Some(call)
		}
	}
	diagnostics := slices.Clone(response.Diagnostics)
	return model.Response{
		Content: items, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: response.ResponseModel,
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
		response, present := entry.Model.Get()
		if !present {
			index++
			continue
		}
		outcome, terminal := response.Outcome.Get()
		if terminal && (outcome == model.OutcomeAborted || outcome == model.OutcomeFailed) {
			index++
			continue
		}

		projected = append(projected, cloneHistoryEntry(entry))
		results := make(map[string]agent.HistoryEntry)
		next := index + 1
		for next < len(history) && history[next].Kind == agent.HistoryEntryToolResult {
			result, hasResult := history[next].ToolResult.Get()
			if hasResult {
				results[result.CallID] = history[next]
			}
			next++
		}
		for _, call := range modelToolCalls(response) {
			if stored, exists := results[call.ID]; exists {
				projected = append(projected, cloneHistoryEntry(stored))
				continue
			}
			projected = append(projected, agent.HistoryEntry{
				Kind:  agent.HistoryEntryToolResult,
				User:  mo.None[model.Message](),
				Model: mo.None[model.Response](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(skippedCallError), IsError: true,
				}),
			})
		}
		// Results without a finalized model call violate the linked-result invariant and do not enter provider history.
		index = next
	}
	return projected
}

// modelToolCalls returns finalized calls in model-provided order.
func modelToolCalls(response model.Response) []model.ToolCall {
	return lo.FilterMap(response.Content, func(item model.Content, _ int) (model.ToolCall, bool) {
		call, present := item.ToolCall.Get()
		return call, item.Kind == model.ContentToolCall && present
	})
}
