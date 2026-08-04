package run

import "github.com/n-r-w/glyph/host/internal/domain/agent"

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
		User:       entry.User,
		Model:      cloneModelResponse(entry.Model),
		ToolResult: entry.ToolResult,
	}
}

// cloneModelResponse preserves ordered content while isolating mutable values.
func cloneModelResponse(response agent.ModelResponse) agent.ModelResponse {
	items := make([]agent.ModelItem, len(response.Items))
	for index, item := range response.Items {
		items[index] = agent.ModelItem{
			Kind: item.Kind,
			Text: item.Text,
			ProviderContext: agent.ProviderContext{
				ProviderID: item.ProviderContext.ProviderID,
				Payload:    append([]byte(nil), item.ProviderContext.Payload...),
			},
			ToolCall: agent.ToolCall{
				ID:        item.ToolCall.ID,
				Name:      item.ToolCall.Name,
				Arguments: cloneArguments(item.ToolCall.Arguments),
			},
		}
	}
	return agent.ModelResponse{Items: items, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage}
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
		if entry.Model.Outcome == agent.ModelOutcomeAborted || entry.Model.Outcome == agent.ModelOutcomeFailed {
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
				User:  agent.UserMessage{Text: ""},
				Model: agent.ModelResponse{Items: nil, Outcome: 0, ErrorMessage: ""},
				ToolResult: agent.ToolResult{
					CallID: call.ID, ToolName: call.Name, Content: skippedCallError, IsError: true,
				},
			})
		}
		index = next
	}
	return projected
}

// modelToolCalls returns finalized calls in model-provided order.
func modelToolCalls(response agent.ModelResponse) []agent.ToolCall {
	calls := make([]agent.ToolCall, 0)
	for _, item := range response.Items {
		if item.Kind == agent.ModelItemToolCall {
			calls = append(calls, item.ToolCall)
		}
	}
	return calls
}
