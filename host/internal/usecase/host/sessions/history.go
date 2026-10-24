package sessions

import (
	"errors"
	"fmt"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
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
		Compaction:       mo.None[session.CompactionEntry](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
	switch history.Kind {
	case agent.HistoryEntryUser:
		message, present := history.User.Get()
		if !present {
			return session.Entry{}, false, errors.New("user history payload is missing")
		}
		entry.User = mo.Some(message.Clone())
	case agent.HistoryEntryModel:
		response, present := history.Model.Get()
		if !present {
			return session.Entry{}, false, errors.New("model history payload is missing")
		}
		outcome, terminal := response.Outcome.Get()
		if !terminal || !outcome.Valid() {
			return session.Entry{}, false, nil
		}
		entry.Model = mo.Some(response.Clone())
	case agent.HistoryEntryToolResult:
		result, present := history.ToolResult.Get()
		if !present {
			return session.Entry{}, false, errors.New("tool-result history payload is missing")
		}
		entry.ToolResult = mo.Some(result.Clone())
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
		projected := historyFromEntries(entries[index : index+1]).Content()
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

// storedCompactedHistoryFromEntries rebuilds the model context from the active branch's latest compaction.
func storedCompactedHistoryFromEntries(entries []session.Entry) []storedHistoryEntry {
	projected := projectSessionHistory(compactedHistoryFromEntries(entries)).Content()
	history := make([]storedHistoryEntry, len(projected))
	for index := range projected {
		history[index] = storedHistoryEntry{value: projected[index], clientVisible: true}
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
