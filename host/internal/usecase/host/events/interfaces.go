package events

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=events

// ClientDelivery sends lifecycle facts to the active client output.
type ClientDelivery interface {
	// DeliverAgent completes one agent event delivery.
	DeliverAgent(context.Context, agent.Event) error
	// DeliverSettled clears the finished run's output state and delivers settlement.
	DeliverSettled(context.Context, string) error
}

// Observer synchronously handles Agent Core and Host settlement lifecycle events.
type Observer interface {
	// Observe handles one Agent Core event before the next source event can proceed.
	Observe(context.Context, agent.Event) error
	// ObserveSettled handles one Host settlement event.
	ObserveSettled(context.Context, string) error
}
