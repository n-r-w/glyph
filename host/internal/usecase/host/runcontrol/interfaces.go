package runcontrol

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=runcontrol

// Request starts one Host-identified user run.
type Request struct {
	// RunID identifies the Host-prepared run.
	RunID string
	// UserText contains the submitted user request.
	UserText string
}

// Result reports the terminal outcome and the execution owner's settlement transition.
type Result struct {
	// Outcome identifies the terminal run state.
	Outcome agent.RunOutcome
	// SettlementRequired reports that execution entered awaiting-settlement state.
	SettlementRequired bool
}

// Executor owns Core execution state and its terminal settlement transition.
type Executor interface {
	// Run executes one prepared request and reports whether this run needs settlement.
	Run(context.Context, Request) (Result, error)
	// Settle makes the matching finished run idle.
	Settle(runID string) error
}

// SettledDelivery completes client output and subsequent settlement observers.
type SettledDelivery interface {
	// DeliverSettled waits for all settlement recipients before returning.
	DeliverSettled(context.Context, string) error
}

// Gate reserves run execution against session mutation.
type Gate interface {
	// TryAcquire returns a reservation release function when admission succeeds.
	TryAcquire() (release func(), acquired bool)
}
