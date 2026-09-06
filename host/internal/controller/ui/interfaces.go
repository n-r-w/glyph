package ui

import (
	"context"

	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=ui

// Session owns initialization policy and prepared application execution.
type Session interface {
	// Initialize publishes the assembled initial state before runtime activation.
	Initialize(context.Context) error
	// Activate starts asynchronous readiness work and returns its cancel-and-join action.
	Activate(context.Context) func()
	// Prepare validates application admission without starting command work.
	Prepare(context.Context, Command) (operation.Prepared[Frame, Frame], error)
}

// PreparationFailure gives input processing the closed rejection category and full cause.
type PreparationFailure interface {
	error
	PreparationCode() string
}

// InitializationFailure distinguishes rejected initialization from broken stream transport.
type InitializationFailure interface {
	error
	InitializationCode() string
}

// StreamSource opens the selected process stream without starting another process.
type StreamSource interface {
	Open(context.Context) (Connection, error)
}

// Connection supplies stream I/O and one attached ordered output owner.
type Connection interface {
	Recv() (*uiv1.OpenResponse, error)
	CloseSend() error
	AttachOutput(context.Context, func(error)) (*operation.Writer[*uiv1.OpenRequest], OperationOutput)
	ClearOutput()
}

// StartupOutput rejects commands received before initialization completes.
type StartupOutput interface {
	Reject(id, code string, cause error) error
}

// OperationOutput maps operation lifecycle events and input rejections to the stream.
type OperationOutput interface {
	operation.Delivery[Frame, OperationResult]
	// SetKind records the command kind for terminal mapping and diagnostics.
	SetKind(id, kind string)
	// TakeKind removes metadata after a rejected preparation.
	TakeKind(id string) string
	// Reject queues a rejected input without claiming an operation identifier.
	Reject(id, code string, cause error) error
	// CloseConnection enqueues the Host close request after command receipt stops.
	CloseConnection() (*operation.Acknowledgement, error)
}
