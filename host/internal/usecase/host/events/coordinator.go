package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

const runIDBytes = 16

// Coordinator owns run IDs, operation-gate reservations, and Agent Core settlement for one Host process.
type Coordinator struct {
	execute  func(context.Context, run.Request) (run.Result, error)
	settle   func(string) error
	events   *Dispatcher
	newRunID func() (string, error)
	// gate is shared with session replacement across headless, UI, and Programmatic Control paths.
	gate OperationGate
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
	execute func(context.Context, run.Request) (run.Result, error),
	settle func(string) error,
	events *Dispatcher,
	gate OperationGate,
) *Coordinator {
	return newCoordinator(execute, settle, events, generateRunID, gate)
}

// newCoordinator creates a coordinator with deterministic package-test seams.
func newCoordinator(
	execute func(context.Context, run.Request) (run.Result, error),
	settle func(string) error,
	events *Dispatcher,
	newRunID func() (string, error),
	gate OperationGate,
) *Coordinator {
	return &Coordinator{
		execute:  execute,
		settle:   settle,
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

// RunPrepared executes one run whose Host ID was allocated before acceptance.
func (c *Coordinator) RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error) {
	release := c.takePrepared(runID)
	if release == nil {
		return 0, errors.New("run is not prepared")
	}
	defer release()
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
