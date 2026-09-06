// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensioncontext

// RuntimeState supplies the currently accepted process identity.
type RuntimeState interface {
	// ContextRuntime returns the runtime instance and its availability.
	ContextRuntime(extensionID string) (instanceID string, available bool)
	// BeginContextCommit protects one final state-owner commit from runtime invalidation.
	BeginContextCommit(extensionID, runtimeID string) (release func(), err error)
}

// SessionState supplies one atomic active-session identity snapshot.
type SessionState interface {
	// ContextSession returns durable identity, project directory, and process-local incarnation.
	ContextSession() SessionIdentity
	// AppendExtension appends one entry only while the expected session incarnation remains active.
	AppendExtension(
		context.Context,
		SessionIdentity,
		session.ExtensionEnvelope,
		ContextCommitGuard,
	) (session.Entry, error)
	// AppendExtensionMessage persists and publishes one message under the bound session incarnation.
	AppendExtensionMessage(
		context.Context,
		SessionIdentity,
		session.ExtensionMessage,
		ContextCommitGuard,
	) (session.Entry, error)
	// ExtensionState returns one coherent filtered active-branch snapshot.
	ExtensionState(context.Context, SessionIdentity, string) (SessionSnapshot, error)
}

// ContextCommitGuard acquires runtime validity across one owning session commit.
type ContextCommitGuard func() (release func(), err error)

// SessionSnapshot contains one coherent active-branch view from the session consumer boundary.
type SessionSnapshot struct {
	// SessionID identifies the active durable session read with the entries.
	SessionID session.ID
	// ActiveLeafID identifies the active leaf read with the entries.
	ActiveLeafID mo.Option[string]
	// Entries contains matching extension entries in root-first branch order.
	Entries []session.Entry
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
