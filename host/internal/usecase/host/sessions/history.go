package sessions

import (
	"fmt"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

func terminalContinuationEntry(history agent.HistoryEntry) (session.Entry, bool, error) {
	entry := session.Entry{
		ParentID:         mo.None[string](),
		ID:               "",
		CreatedAt:        time.Time{},
		Information:      mo.None[session.Information](),
		User:             mo.None[session.UserMessage](),
		Model:            mo.None[session.ModelResponse](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		EstimatedCost:    mo.None[session.EstimatedCost](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
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

// storedHistoryEntry keeps client presentation metadata beside one Agent Core history value.
type storedHistoryEntry struct {
	// value is one provider-neutral history entry owned by the session service.
	value agent.HistoryEntry
	// clientVisible reports whether ordinary client history includes the value.
	clientVisible bool
}

// storedHistoryFromEntries rebuilds one session-owned history from durable branch entries.
func storedHistoryFromEntries(entries []session.Entry) []storedHistoryEntry {
	history := make([]storedHistoryEntry, 0, len(entries))
	for index := range entries {
		projected := sessiontree.HistoryFromEntries(entries[index : index+1])
		clientVisible := true
		if message, present := entries[index].ExtensionMessage.Get(); present {
			clientVisible = message.Visibility == session.ClientVisibilityVisible
		}
		for projectionIndex := range projected {
			history = append(history, storedHistoryEntry{
				value: projected[projectionIndex], clientVisible: clientVisible,
			})
		}
	}
	return history
}

// cloneStoredHistory returns complete or ordinary client history with independent payload ownership.
func cloneStoredHistory(history []storedHistoryEntry, clientOnly bool) []agent.HistoryEntry {
	if history == nil {
		return nil
	}
	cloned := make([]agent.HistoryEntry, 0, len(history))
	for index := range history {
		if clientOnly && !history[index].clientVisible {
			continue
		}
		value, _ := history[index].value.ValidatedClone()
		cloned = append(cloned, value)
	}
	return cloned
}
