// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensioncontext

// RuntimeState supplies the currently accepted process identity.
type RuntimeState interface {
	// ContextRuntime returns the runtime instance and its availability.
	ContextRuntime(extensionID string) (instanceID string, available bool)
}

// SessionState supplies one atomic active-session identity snapshot.
type SessionState interface {
	// ContextSession returns durable identity, project directory, and process-local incarnation.
	ContextSession() SessionIdentity
	// AppendExtension appends one entry only while the expected session incarnation remains active.
	AppendExtension(context.Context, SessionIdentity, session.ExtensionEnvelope) (session.Entry, error)
	// ExtensionState returns one coherent filtered active-branch snapshot.
	ExtensionState(context.Context, SessionIdentity, string) (session.ExtensionStateSnapshot, error)
}

// SessionIdentity distinguishes active incarnations of the same durable session.
type SessionIdentity struct {
	// ID identifies the durable active session.
	ID string
	// WorkingDirectory identifies the session project.
	WorkingDirectory string
	// Incarnation changes on every successful active-session replacement.
	Incarnation uint64
}

// Catalog supplies provider-neutral descriptors, active selection, and explicit configured requests.
type Catalog interface {
	// Models returns defensive descriptors in configured order.
	Models() []model.Descriptor
	// ActiveSelection returns the complete active selection.
	ActiveSelection() model.Selection
	// Request executes one explicit configured selection without changing active selection.
	Request(
		ctx context.Context,
		selection model.Selection,
		instructions string,
		history []agent.HistoryEntry,
	) (model.Response, error)
}

// RequestFailure exposes a provider-owned configured-request failure category.
type RequestFailure interface {
	error
	// SelectionCode returns the provider catalog failure code.
	SelectionCode() string
}
