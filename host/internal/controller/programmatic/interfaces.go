package programmatic

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// HostSession executes transport-independent commands for one controller connection.
type HostSession interface {
	Handle(ctx context.Context, command Command) error
	CancelAndWait(ctx context.Context) error
}
