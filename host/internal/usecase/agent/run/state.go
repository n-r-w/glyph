package run

import "github.com/n-r-w/glyph/host/internal/domain/agent"

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
	Status          Status
	RunID           string
	PartialResponse agent.ModelResponse
}

// Request starts one Host-identified user run.
type Request struct {
	RunID    string
	UserText string
}

// Result is the terminal Agent Core run result.
type Result struct {
	Outcome      agent.RunOutcome
	AddedHistory []agent.HistoryEntry
	ErrorMessage string
}
