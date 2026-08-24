package programmatic

import (
	"context"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// Operation exposes one accepted user request to its sole controller consumer.
type Operation interface {
	// Start begins execution once and does nothing after execution or cancellation starts.
	Start()
	// Events returns the same unbuffered stream for the operation lifetime.
	Events() <-chan AgentEvent
}

// HostSession executes transport-independent commands for one controller connection.
type HostSession interface {
	Handle(ctx context.Context, command Command) (Response, Operation, error)
	CancelAndWait(ctx context.Context) error
}

// OpenStream is the gRPC stream surface used by the controller.
type OpenStream interface {
	Context() context.Context
	Recv() (*programmaticv1.OpenRequest, error)
	Send(*programmaticv1.OpenResponse) error
}
