//go:build !integration

package programmatic

import (
	"context"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
)

func newTestActiveRun(
	ctx context.Context,
	delivery *Delivery,
	correlationID string,
	runID string,
) *activeRun {
	runContext, cancel := context.WithCancel(ctx)
	return &activeRun{
		coordinator:   nil,
		userText:      "",
		streamStopped: false,
		err:           nil,
		delivery:      delivery,
		correlationID: correlationID,
		runID:         runID,
		runContext:    runContext,
		cancel:        cancel,
		events:        make(chan controller.AgentEvent),
		streamDone:    make(chan struct{}),
		done:          make(chan struct{}),
		state:         operationRunning,
	}
}
