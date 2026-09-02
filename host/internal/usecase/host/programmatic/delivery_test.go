//go:build !integration

package programmatic

import (
	"context"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
)

// newTestActiveRun creates an accepted run for delivery state tests.
func newTestActiveRun(
	ctx context.Context,
	delivery *Delivery,
	operationID string,
	runID string,
) *activeRun {
	runContext, cancel := context.WithCancel(ctx)
	return &activeRun{
		coordinator:   nil,
		userText:      "",
		streamStopped: false,
		err:           nil,
		delivery:      delivery,
		operationID:   operationID,
		runID:         runID,
		runContext:    runContext,
		cancel:        cancel,
		events:        make(chan controller.AgentEvent),
		streamDone:    make(chan struct{}),
		done:          make(chan struct{}),
		state:         operationRunning,
	}
}
