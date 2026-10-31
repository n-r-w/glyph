// Package runcontrol owns prepared run admission and settlement ordering.
package runcontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	"github.com/n-r-w/glyph/internal/errtree"
)

// runIDBytes is the entropy size of a Host run identifier.
const runIDBytes = 16

// Coordinator owns run IDs, operation-gate reservations, and Agent Core settlement for one Host process.
type Coordinator struct {
	// executor runs requests and completes the Core settlement transition.
	executor Executor
	// input produces the final user message before Core execution.
	input InputProcessor
	// events completes client settlement delivery and subsequent observation.
	events SettledDelivery
	// newRunID creates one opaque run identifier.
	newRunID func() (string, error)
	// gate reserves agent execution against session mutation.
	gate Gate
	// mutex protects transfer of prepared reservation ownership to execution or cancellation.
	mutex sync.Mutex
	// prepared stores each release function until exactly one terminal owner takes it.
	prepared map[string]func()
}

var (
	_ hostui.AgentRunner           = (*Coordinator)(nil)
	_ headless.AgentRunner         = (*Coordinator)(nil)
	_ hostprogrammatic.Coordinator = (*Coordinator)(nil)
)

// NewCoordinator creates the production Host run coordinator.
func NewCoordinator(
	executor Executor,
	input InputProcessor,
	events SettledDelivery,
	gate Gate,
) *Coordinator {
	return newCoordinator(executor, input, events, generateRunID, gate)
}

// newCoordinator creates a coordinator with deterministic package-test seams.
func newCoordinator(
	executor Executor,
	input InputProcessor,
	events SettledDelivery,
	newRunID func() (string, error),
	gate Gate,
) *Coordinator {
	return &Coordinator{
		executor: executor,
		input:    input,
		events:   events,
		newRunID: newRunID,
		gate:     gate,
		mutex:    sync.Mutex{},
		prepared: make(map[string]func()),
	}
}

// PrepareRun allocates one Host-owned run ID before a controller accepts the run.
func (c *Coordinator) PrepareRun() (string, error) {
	release, acquired := c.gate.TryAcquire()
	if !acquired {
		return "", session.ErrBusy
	}
	runID, err := c.newRunID()
	if err != nil {
		release()
		return "", fmt.Errorf("create Host run ID: %w", err)
	}
	c.mutex.Lock()
	c.prepared[runID] = release
	c.mutex.Unlock()
	return runID, nil
}

// CancelPrepared releases a run reservation when acceptance delivery fails.
func (c *Coordinator) CancelPrepared(runID string) {
	if release := c.takePrepared(runID); release != nil {
		// Release is idempotent because cancellation can race with stream teardown.
		release()
	}
}

// RunPrepared processes the submitted input and executes one run whose Host ID was allocated before acceptance,
// or completes a fully handled input without Core execution.
func (c *Coordinator) RunPrepared(ctx context.Context, runID string, message model.Message) (agent.RunOutcome, error) {
	release := c.takePrepared(runID)
	if release == nil {
		return 0, errors.New("run is not prepared")
	}
	defer release()
	processed, err := c.input.ProcessInput(ctx, message)
	if err != nil {
		outcome := agent.RunOutcomeFailed
		if isOwningCancellation(ctx, err) {
			// Pure owner cancellation follows the aborted-run path, as when Core observes it.
			outcome = agent.RunOutcomeAborted
		}
		return outcome, fmt.Errorf("process user input: %w", err)
	}
	final, present := processed.Get()
	if !present {
		// A fully handled input completes the run without Core execution or settlement.
		return agent.RunOutcomeCompleted, nil
	}
	result, runErr := c.executor.Run(ctx, Request{RunID: runID, Message: final})
	if !result.SettlementRequired {
		return result.Outcome, runErr
	}
	// Core reports its own transition, including cancellation before the first append.
	terminalContext := context.WithoutCancel(ctx)
	settleErr := c.executor.Settle(runID)
	settledErr := c.events.DeliverSettled(terminalContext, runID)
	if settleErr != nil || settledErr != nil {
		result.Outcome = agent.RunOutcomeFailed
	}
	return result.Outcome, errors.Join(runErr, settleErr, settledErr)
}

// Run executes one Agent Core run, emits Host settlement, and makes Agent Core idle.
func (c *Coordinator) Run(ctx context.Context, message model.Message) (agent.RunOutcome, error) {
	runID, err := c.PrepareRun()
	if err != nil {
		return 0, err
	}
	return c.RunPrepared(ctx, runID, message)
}

// isOwningCancellation reports a failure whose current cause is only the cancellation of the owning context.
func isOwningCancellation(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	return errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, ctx.Err()) || errors.Is(cause, context.Cause(ctx))
	})
}

// takePrepared atomically transfers reservation cleanup to execution or cancellation.
func (c *Coordinator) takePrepared(runID string) func() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	release := c.prepared[runID]
	delete(c.prepared, runID)
	return release
}

// generateRunID creates one nonempty process-local unique run identifier.
func generateRunID() (string, error) {
	data := make([]byte, runIDBytes)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("read secure randomness: %w", err)
	}
	return hex.EncodeToString(data), nil
}
