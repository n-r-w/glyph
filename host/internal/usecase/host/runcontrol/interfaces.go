package runcontrol

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=runcontrol

// Request starts one Host-identified user run.
type Request struct {
	// RunID identifies the Host-prepared run.
	RunID string
	// Message contains the final user message that Core appends.
	Message model.Message
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

// InputProcessor produces the final user message of an accepted run before Core execution.
type InputProcessor interface {
	// ProcessInput returns the final message, no message when an extension fully handled the input,
	// or an error that fails the run before Core execution.
	ProcessInput(context.Context, model.Message) (mo.Option[model.Message], error)
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
