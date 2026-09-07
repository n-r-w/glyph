package plugin

import (
	"context"

	"github.com/samber/mo"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=plugin

// Presentation consumes validated initialization and serialized Host notifications.
type Presentation interface {
	PrepareInitialize(Initialization) (InitializationOperation, error)
	Notify(Notification) error
	Run(context.Context) error
	Close() error
}

// InitializationOperation owns application initialization after admission.
type InitializationOperation interface {
	Run(context.Context) error
	Release()
}

// Initialization contains validated startup facts without operation or rendering state.
type Initialization struct {
	// Availability is the initial Host admission state.
	Availability Availability
	// Startup contains ordered startup diagnostics.
	Startup []Transcript
	// Models contains the configured selection choices.
	Models []ConfiguredModel
	// Selection is the Host-confirmed active model.
	Selection ModelSelection
	// Session identifies the active session.
	Session SessionInfo
}

// NotificationKind identifies an operation stage or unsolicited update.
type NotificationKind uint8

const (
	// NotificationProgress carries running operation data.
	NotificationProgress NotificationKind = iota
	// NotificationCompleted releases a completed operation.
	NotificationCompleted
	// NotificationFailed carries an operation source failure.
	NotificationFailed
	// NotificationConnection carries an unsolicited update.
	NotificationConnection
)

// Notification contains correlation and validated data, not display policy.
type Notification struct {
	// Kind identifies the stage of the incoming notification.
	Kind NotificationKind
	// OperationID identifies an operation, or is empty for connection updates.
	OperationID string
	// Payload contains the validated update when the stage has one.
	Payload mo.Option[Payload]
	// Failure retains the source error when the operation failed.
	Failure error
}
