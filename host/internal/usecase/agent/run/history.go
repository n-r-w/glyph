package run

import (
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
		cloned[index] = cloned[index].Clone()
	}
	return cloned
}

// cloneToolPreviews isolates mutable preview fields.
func cloneToolPreviews(previews map[string]model.ToolCallPreview) map[string]model.ToolCallPreview {
	if previews == nil {
		return nil
	}
	cloned := maps.Clone(previews)
	for callID, preview := range cloned {
		cloned[callID] = preview.Clone()
	}
	return cloned
}

// ProjectHistory excludes failed responses and supplies temporary skipped results.
func ProjectHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	projected := make([]agent.HistoryEntry, 0, len(history))
	for index := 0; index < len(history); {
		entry := history[index]
		if entry.Kind != agent.HistoryEntryModel {
			projected = append(projected, entry.Clone())
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

		projected = append(projected, entry.Clone())
		calls := modelToolCalls(response)
		results := make(map[string]agent.HistoryEntry, len(calls))
		intervening := make([]agent.HistoryEntry, 0)
		next := index + 1
		// A later model response, not an intervening message, delimits ownership of persisted tool results.
		for next < len(history) && history[next].Kind != agent.HistoryEntryModel {
			candidate := history[next]
			if candidate.Kind != agent.HistoryEntryToolResult {
				intervening = append(intervening, candidate.Clone())
				next++
				continue
			}
			result, hasResult := candidate.ToolResult.Get()
			if hasResult {
				results[result.CallID] = candidate
			}
			next++
		}
		for _, call := range calls {
			if stored, exists := results[call.ID]; exists {
				projected = append(projected, stored.Clone())
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
		projected = append(projected, intervening...)
		// Results without a finalized call owned by this response do not enter provider history.
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
