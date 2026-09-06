// Package events dispatches client events before lifecycle observers.
package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// Dispatcher sends Agent Core and Host settlement events to the client and lifecycle observers.
type Dispatcher struct {
	// client delivers agent and settlement events to the active recipient.
	client ClientDelivery
	// observers owns ordered extension lifecycle observation.
	observers Observer
}

var (
	_ run.EventSink              = (*Dispatcher)(nil)
	_ runcontrol.SettledDelivery = (*Dispatcher)(nil)
)

// NewDispatcher creates one synchronous Host event dispatcher.
func NewDispatcher(
	client ClientDelivery,
	observers Observer,
) *Dispatcher {
	return &Dispatcher{client: client, observers: observers}
}

// Deliver forwards one Agent Core event without a queue or retry.
func (d *Dispatcher) Deliver(ctx context.Context, event agent.Event) error {
	var clientErr error
	if err := d.client.DeliverAgent(ctx, event); err != nil {
		clientErr = fmt.Errorf("deliver Agent Core event %d: %w", event.Type, err)
	}
	var observerErr error
	if d.observers != nil {
		observerErr = d.observers.Observe(ctx, event)
	}
	return errors.Join(clientErr, observerErr)
}

// DeliverSettled emits one Host-owned settlement event.
func (d *Dispatcher) DeliverSettled(ctx context.Context, runID string) error {
	var clientErr error
	if err := d.client.DeliverSettled(ctx, runID); err != nil {
		clientErr = fmt.Errorf("deliver agent_settled for run %q: %w", runID, err)
	}
	var observerErr error
	if d.observers != nil {
		observerErr = d.observers.ObserveSettled(ctx, runID)
	}
	return errors.Join(clientErr, observerErr)
}
