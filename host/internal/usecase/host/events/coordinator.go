package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

const runIDBytes = 16

// Coordinator owns run IDs and Agent Core settlement for one headless process.
type Coordinator struct {
	execute  func(context.Context, run.Request) (run.Result, error)
	settle   func(string) error
	events   *Dispatcher
	newRunID func() (string, error)
}

var (
	_ hostui.AgentRunner           = (*Coordinator)(nil)
	_ headless.AgentRunner         = (*Coordinator)(nil)
	_ hostprogrammatic.Coordinator = (*Coordinator)(nil)
)

// NewCoordinator creates the production Host run coordinator.
func NewCoordinator(
	execute func(context.Context, run.Request) (run.Result, error),
	settle func(string) error,
	events *Dispatcher,
) *Coordinator {
	return newCoordinator(execute, settle, events, generateRunID)
}

// newCoordinator creates a coordinator with deterministic package-test seams.
func newCoordinator(
	execute func(context.Context, run.Request) (run.Result, error),
	settle func(string) error,
	events *Dispatcher,
	newRunID func() (string, error),
) *Coordinator {
	return &Coordinator{execute: execute, settle: settle, events: events, newRunID: newRunID}
}

// PrepareRun allocates one Host-owned run ID before a controller accepts the run.
func (c *Coordinator) PrepareRun() (string, error) {
	runID, err := c.newRunID()
	if err != nil {
		return "", fmt.Errorf("create Host run ID: %w", err)
	}
	return runID, nil
}

// RunPrepared executes one run whose Host ID was allocated before acceptance.
func (c *Coordinator) RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error) {
	result, runErr := c.execute(ctx, run.Request{RunID: runID, UserText: userText})
	if len(result.AddedHistory) == 0 {
		return result.Outcome, runErr
	}
	terminalContext := context.WithoutCancel(ctx)
	settleErr := c.settle(runID)
	settledErr := c.events.DeliverSettled(terminalContext, runID)
	return result.Outcome, errors.Join(runErr, settleErr, settledErr)
}

// Run executes one Agent Core run, emits Host settlement, and makes Agent Core idle.
func (c *Coordinator) Run(ctx context.Context, userText string) (agent.RunOutcome, error) {
	runID, err := c.PrepareRun()
	if err != nil {
		return 0, err
	}
	return c.RunPrepared(ctx, runID, userText)
}

// generateRunID creates one nonempty process-local unique run identifier.
func generateRunID() (string, error) {
	data := make([]byte, runIDBytes)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("read secure randomness: %w", err)
	}
	return hex.EncodeToString(data), nil
}
