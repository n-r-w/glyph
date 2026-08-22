// Package agent defines provider-neutral Agent Core history and run values.
package agent

import "github.com/n-r-w/glyph/host/internal/domain/model"

// HistoryEntryKind identifies one linear history entry.
type HistoryEntryKind uint8

const (
	// HistoryEntryUser is one user-authored message.
	HistoryEntryUser HistoryEntryKind = iota + 1
	// HistoryEntryModel is one finalized model response.
	HistoryEntryModel
	// HistoryEntryToolResult is one completed tool result.
	HistoryEntryToolResult
)

// HistoryEntry is one ordered session-history item.
type HistoryEntry struct {
	Kind       HistoryEntryKind
	User       model.Message
	Model      model.Response
	ToolResult ToolResult
}

// ToolResult is one model-visible terminal tool result.
type ToolResult struct {
	CallID   string
	ToolName string
	Content  string
	IsError  bool
}

// RunOutcome identifies the terminal Agent Core run state.
type RunOutcome uint8

const (
	// RunOutcomeCompleted ended through a final model outcome.
	RunOutcomeCompleted RunOutcome = iota + 1
	// RunOutcomeAborted ended through cancellation.
	RunOutcomeAborted
	// RunOutcomeFailed ended through provider, tool, or delivery failure.
	RunOutcomeFailed
)
