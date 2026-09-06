package events

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=events

// Observer synchronously handles Agent Core and Host settlement lifecycle events.
type Observer interface {
	// Observe handles one Agent Core event before the next source event can proceed.
	Observe(context.Context, agent.Event) error
	// ObserveSettled handles one Host settlement event.
	ObserveSettled(context.Context, string) error
}
