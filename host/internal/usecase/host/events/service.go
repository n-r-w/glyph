// Package events synchronously dispatches Agent Core events and coordinates Host settlement.
package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// Dispatcher sends Agent Core and Host settlement events to the client and lifecycle observers.
type Dispatcher struct {
	// deliverAgent sends one Agent Core event to the active recipient.
	deliverAgent func(context.Context, run.Event) error
	// deliverSettled sends one Host settlement event to the active recipient.
	deliverSettled func(context.Context, string) error
	// observers owns ordered extension lifecycle observation.
	observers Observer
}

var _ run.EventSink = (*Dispatcher)(nil)

// NewDispatcher creates one synchronous Host event dispatcher.
func NewDispatcher(
	deliverAgent func(context.Context, run.Event) error,
	deliverSettled func(context.Context, string) error,
	observers Observer,
) *Dispatcher {
	return &Dispatcher{deliverAgent: deliverAgent, deliverSettled: deliverSettled, observers: observers}
}

// Deliver forwards one Agent Core event without a queue or retry.
func (d *Dispatcher) Deliver(ctx context.Context, event run.Event) error {
	var clientErr error
	if err := d.deliverAgent(ctx, event); err != nil {
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
	if err := d.deliverSettled(ctx, runID); err != nil {
		clientErr = fmt.Errorf("deliver agent_settled for run %q: %w", runID, err)
	}
	var observerErr error
	if d.observers != nil {
		observerErr = d.observers.ObserveSettled(ctx, runID)
	}
	return errors.Join(clientErr, observerErr)
}
