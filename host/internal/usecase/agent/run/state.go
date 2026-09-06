package run

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// Status identifies Agent Core run availability.
type Status uint8

const (
	// StatusIdle accepts a new run.
	StatusIdle Status = iota + 1
	// StatusRunning has active provider or tool work.
	StatusRunning
	// StatusAwaitingSettlement has emitted agent_end and awaits Host settlement.
	StatusAwaitingSettlement
)

// State is an immutable Agent Core state snapshot.
type State struct {
	// Status identifies the current run lifecycle state.
	Status Status
	// RunID identifies the active or unsettled run.
	RunID mo.Option[string]
	// PartialResponse contains accumulated streamed model content.
	PartialResponse mo.Option[model.Response]
	// ToolPreviews contains provisional tool calls by call ID.
	ToolPreviews map[string]model.ToolCallPreview
}

// turnResult carries the private model/tool loop outcome into the run's terminal transition.
type turnResult struct {
	// Outcome identifies the terminal run state.
	Outcome agent.RunOutcome
	// ErrorMessage contains a terminal failure message.
	ErrorMessage mo.Option[string]
}
