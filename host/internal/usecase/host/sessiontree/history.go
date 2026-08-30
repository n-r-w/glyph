package sessiontree

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// HistoryFromEntries projects model-visible session entries into provider-neutral history.
func HistoryFromEntries(entries []session.Entry) []agent.HistoryEntry {
	history := make([]agent.HistoryEntry, 0, len(entries))
	for index := range entries {
		entry := &entries[index]
		if user, present := entry.User.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(user.Clone()),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if response, present := entry.Model.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(response.Clone()), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if result, present := entry.ToolResult.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](),
				Model: mo.None[model.Response](), ToolResult: mo.Some(result.Clone()),
			})
		}
		if summary, present := entry.BranchSummary.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind:  agent.HistoryEntryUser,
				User:  mo.Some(model.TextMessage(RenderBranchSummaryContext(summary.Summary))),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
	}
	return history
}
