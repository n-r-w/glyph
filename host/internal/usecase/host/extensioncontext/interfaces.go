// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
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
	ContextSession() contextcompaction.SessionIdentity
	// AppendExtension appends one entry only while the expected session incarnation remains active.
	AppendExtension(
		context.Context,
		contextcompaction.SessionIdentity,
		session.ExtensionEnvelope,
		ContextCommitGuard,
	) (session.Entry, error)
	// AppendExtensionMessage persists and publishes one message under the bound session incarnation.
	AppendExtensionMessage(
		context.Context,
		contextcompaction.SessionIdentity,
		session.ExtensionMessage,
		ContextCommitGuard,
	) (session.Entry, error)
	// ExtensionState returns one coherent filtered active-branch snapshot.
	ExtensionState(context.Context, contextcompaction.SessionIdentity, string) (SessionSnapshot, error)
	// ProtectContextCommit validates and protects the expected session before runtime while commit runs.
	ProtectContextCommit(context.Context, contextcompaction.SessionIdentity, ContextCommitGuard, func() error) error
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
