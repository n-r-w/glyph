package run

import (
	"maps"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//nolint:misspell // This exact model-visible error is stored for skipped calls.
const skippedCallError = "Tool call skipped because the agent run was cancelled."

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

// HistoryProjection pairs prepared records with their caller input positions.
type HistoryProjection struct {
	// History contains ordered prepared model records.
	History []agent.HistoryEntry
	// InputPositions contains each prepared record's zero-based caller input position.
	InputPositions []int
}

// ProjectHistory returns prepared history with correspondence to caller input positions.
func ProjectHistory(history []agent.HistoryEntry) HistoryProjection {
	projected := HistoryProjection{
		History:        make([]agent.HistoryEntry, 0, len(history)),
		InputPositions: make([]int, 0, len(history)),
	}
	appendEntry := func(entry agent.HistoryEntry, position int) {
		projected.History = append(projected.History, entry)
		projected.InputPositions = append(projected.InputPositions, position)
	}
	for index := 0; index < len(history); {
		entry := history[index]
		if entry.Kind != agent.HistoryEntryModel {
			appendEntry(entry.Clone(), index)
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

		appendEntry(entry.Clone(), index)
		calls := modelToolCalls(response)
		results := make(map[string]int, len(calls))
		intervening := make([]int, 0)
		next := index + 1
		// Each model response owns persisted tool results up to the next model response.
		for next < len(history) && history[next].Kind != agent.HistoryEntryModel {
			candidate := history[next]
			if candidate.Kind != agent.HistoryEntryToolResult {
				intervening = append(intervening, next)
				next++
				continue
			}
			result, hasResult := candidate.ToolResult.Get()
			if hasResult {
				results[result.CallID] = next
			}
			next++
		}
		for _, call := range calls {
			if stored, exists := results[call.ID]; exists {
				appendEntry(history[stored].Clone(), stored)
				continue
			}
			appendEntry(agent.HistoryEntry{
				Kind:  agent.HistoryEntryToolResult,
				User:  mo.None[model.Message](),
				Model: mo.None[model.Response](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(skippedCallError), IsError: true,
				}),
			}, index)
		}
		for _, position := range intervening {
			appendEntry(history[position].Clone(), position)
		}
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
