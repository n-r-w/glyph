package sessions

import (
	"fmt"
	"slices"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

func terminalContinuationEntry(history agent.HistoryEntry) (session.Entry, bool, error) {
	entry := session.Entry{
		ParentID: mo.None[string](), ID: "", CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model:      mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	switch history.Kind {
	case agent.HistoryEntryUser:
		entry.User = mo.Some(history.User.MustGet().Clone())
	case agent.HistoryEntryModel:
		response := history.Model.MustGet()
		outcome, terminal := response.Outcome.Get()
		if !terminal || !outcome.Valid() {
			return session.Entry{}, false, nil
		}
		entry.Model = mo.Some(response.Clone())
	case agent.HistoryEntryToolResult:
		entry.ToolResult = mo.Some(history.ToolResult.MustGet().Clone())
	default:
		return session.Entry{}, false, fmt.Errorf("unsupported history entry kind %d", history.Kind)
	}
	return entry, true, nil
}

func cloneHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	cloned := slices.Clone(history)
	for index := range cloned {
		cloned[index], _ = cloned[index].ValidatedClone()
	}
	return cloned
}
