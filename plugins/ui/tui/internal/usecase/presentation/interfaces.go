package presentation

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=presentation

// Host dispatches prepared commands through the initialized SDK connection.
type Host interface {
	Send(string, Command, string) error
	CloseConnection(context.Context) error
	StopDispatch()
}

// Display receives coherent immutable view data after serialized state transitions.
type Display interface {
	Publish(Snapshot)
}

// Runtime owns terminal resources and runs the framework event loop.
type Runtime interface {
	Open() error
	Run(context.Context) RuntimeResult
	Close() error
}

// RuntimeResult distinguishes local program completion from connection or parent shutdown.
type RuntimeResult struct {
	// ProgramExited identifies local program completion that requires Host close.
	ProgramExited bool
	// Err contains complete program, mapping, notification, or parent failure text.
	Err error
}
