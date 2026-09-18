package sessions

import (
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

const (
	// branchSummaryContextOpening starts persisted summary data in active provider history.
	branchSummaryContextOpening = "<summary encoding=\"xml-text\">\n"
	// branchSummaryContextClosing ends persisted summary data in active provider history.
	branchSummaryContextClosing = "\n</summary>"
)

// historyFromEntries projects model-visible session entries into provider-neutral history.
func historyFromEntries(entries []session.Entry) []agent.HistoryEntry {
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
		if message, present := entry.ExtensionMessage.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(message.Text)),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if summary, present := entry.BranchSummary.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind:  agent.HistoryEntryUser,
				User:  mo.Some(model.TextMessage(renderBranchSummaryContext(summary.Summary))),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
	}
	return history
}

// compactedHistoryFromEntries projects the active branch after applying its latest compaction marker.
func compactedHistoryFromEntries(entries []session.Entry) []agent.HistoryEntry {
	latestIndex := -1
	for index := range slices.Backward(entries) {
		if entries[index].Compaction.IsSome() {
			latestIndex = index
			break
		}
	}
	if latestIndex < 0 {
		return historyFromEntries(entries)
	}
	compaction := entries[latestIndex].Compaction.MustGet()
	boundaryIndex := -1
	for index := range entries {
		if entries[index].ID == compaction.FirstKeptEntryID {
			boundaryIndex = index
			break
		}
	}
	if boundaryIndex < 0 {
		return nil
	}
	history := []agent.HistoryEntry{{
		Kind:  agent.HistoryEntryUser,
		User:  mo.Some(model.TextMessage(renderBranchSummaryContext(compaction.Summary))),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	for index := boundaryIndex; index < len(entries); index++ {
		if entries[index].Compaction.IsSome() {
			continue
		}
		history = append(history, historyFromEntries(entries[index:index+1])...)
	}
	return history
}

// renderBranchSummaryContext encodes persisted summary data for one provider user-role history message.
func renderBranchSummaryContext(summary string) string {
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(summary)
	return branchSummaryContextOpening + escaped + branchSummaryContextClosing
}
