package sessions

import (
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

const (
	// branchSummaryContextOpening starts persisted summary data in active provider history.
	branchSummaryContextOpening = "<summary encoding=\"xml-text\">\n"
	// branchSummaryContextClosing ends persisted summary data in active provider history.
	branchSummaryContextClosing = "\n</summary>"
)

// historyFromEntries projects model-visible session entries into provider-neutral history.
func historyFromEntries(entries []session.Entry) session.PreparedHistory {
	history := make(session.PreparedHistory, 0, len(entries))
	appendEntry := func(value agent.HistoryEntry, index int, sourceKind session.HistorySourceKind) {
		history = append(
			history,
			session.PreparedHistoryEntry{
				History:         value,
				SourceID:        entries[index].ID,
				SourcePosition:  int64(index),
				SourceKind:      sourceKind,
				PreviousSummary: false,
			},
		)
	}
	for index := range entries {
		entry := &entries[index]
		if user, present := entry.User.Get(); present {
			appendEntry(agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(user.Clone()),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			}, index, session.HistorySourceUser)
		}
		if response, present := entry.Model.Get(); present {
			appendEntry(agent.HistoryEntry{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(response.Clone()), ToolResult: mo.None[agent.ToolResult](),
			}, index, session.HistorySourceModel)
		}
		if result, present := entry.ToolResult.Get(); present {
			appendEntry(agent.HistoryEntry{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](),
				Model: mo.None[model.Response](), ToolResult: mo.Some(result.Clone()),
			}, index, session.HistorySourceToolResult)
		}
		if message, present := entry.ExtensionMessage.Get(); present {
			appendEntry(agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(message.Text)),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			}, index, session.HistorySourceExtensionMessage)
		}
		if summary, present := entry.BranchSummary.Get(); present {
			appendEntry(agent.HistoryEntry{
				Kind:  agent.HistoryEntryUser,
				User:  mo.Some(model.TextMessage(renderBranchSummaryContext(summary.Summary))),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			}, index, session.HistorySourceBranchSummary)
		}
	}
	return history
}

// compactedHistoryFromEntries projects the active branch after applying its latest compaction marker.
func compactedHistoryFromEntries(entries []session.Entry) session.PreparedHistory {
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
	history := session.PreparedHistory{{
		History: agent.HistoryEntry{
			Kind:  agent.HistoryEntryUser,
			User:  mo.Some(model.TextMessage(renderBranchSummaryContext(compaction.Summary))),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		},
		SourceID: entries[latestIndex].ID, SourcePosition: int64(latestIndex),
		SourceKind: session.HistorySourceCompaction, PreviousSummary: true,
	}}
	for index := boundaryIndex; index < len(entries); index++ {
		if entries[index].Compaction.IsSome() {
			continue
		}
		converted := historyFromEntries(entries[index : index+1])
		for position := range converted {
			value := converted[position]
			value.SourcePosition = int64(index)
			history = append(history, value)
		}
	}
	return history
}

// renderBranchSummaryContext encodes persisted summary data for one provider user-role history message.
func renderBranchSummaryContext(summary string) string {
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(summary)
	return branchSummaryContextOpening + escaped + branchSummaryContextClosing
}

// projectSessionHistory binds shared projection outputs to their captured session sources.
func projectSessionHistory(input session.PreparedHistory) session.PreparedHistory {
	projection := agentrun.ProjectHistory(input.Content())
	output := make(session.PreparedHistory, len(projection.History))
	for index := range projection.History {
		source := input[projection.InputPositions[index]]
		source.History = projection.History[index]
		output[index] = source
	}
	return output
}
