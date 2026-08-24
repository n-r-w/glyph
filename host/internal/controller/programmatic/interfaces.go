package programmatic

import "context"

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
