// Package agent defines provider-neutral Agent Core history and run values.
package agent

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

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
	User       mo.Option[model.Message]
	Model      mo.Option[model.Response]
	ToolResult mo.Option[ToolResult]
}

// ToolResult is one model-visible terminal tool result.
type ToolResult struct {
	CallID   string
	ToolName string
	Contents []tool.ResultContent
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
