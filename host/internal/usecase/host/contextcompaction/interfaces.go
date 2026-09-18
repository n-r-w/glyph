// Package contextcompaction owns conversation context sizing and compaction coordination.
package contextcompaction

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=contextcompaction

// SessionIdentity identifies one process-local incarnation of a durable session.
type SessionIdentity struct {
	// ID identifies the durable active session.
	ID string
	// WorkingDirectory identifies the session project.
	WorkingDirectory string
	// Incarnation changes on every successful active-session replacement.
	Incarnation uint64
}

// SessionState supplies active-session identity and compaction persistence.
type SessionState interface {
	// ContextSession returns one atomic active-session identity snapshot.
	ContextSession() SessionIdentity
	// CommitCompaction validates and appends one compaction entry to the expected active branch.
	CommitCompaction(
		ctx context.Context,
		expected SessionIdentity,
		expectedLeafID mo.Option[string],
		compaction session.CompactionEntry,
	) (session.Entry, error)
}
