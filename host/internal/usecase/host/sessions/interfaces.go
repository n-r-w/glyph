// Package sessions owns the active session and its persisted lifecycle information.
package sessions

import (
	"context"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessions

// Repository stores and loads sessions for one canonical working directory.
type Repository interface {
	// Initialize creates private project storage before clients start.
	Initialize(context.Context) error
	// Append writes and synchronizes one complete lifecycle record.
	Append(context.Context, AppendCommand) (AppendResult, error)
	// List returns files that pass validation for this project.
	List(context.Context) ([]LoadedSession, error)
	// Load resolves an opaque ID through validated headers rather than paths.
	Load(context.Context, session.ID) (LoadedSession, error)
}

// AppendCommand contains the previous storage path and one entry to persist.
type AppendCommand struct {
	// Header is written only when StoragePath is empty.
	Header session.Header
	// StoragePath selects the existing file for later entries.
	StoragePath string
	// Entry is the complete lifecycle record to append.
	Entry session.Entry
}

// AppendResult contains the path used for a successful append.
type AppendResult struct {
	// StoragePath is the created or reused session file path.
	StoragePath string
}

// LoadedSession contains one validated session file.
type LoadedSession struct {
	// Header contains the validated session identity and project binding.
	Header session.Header
	// StoragePath is the validated JSONL file path.
	StoragePath string
	// Entries preserves validated file order.
	Entries []session.Entry
}

// IDGenerator creates opaque session and entry identifiers.
type IDGenerator interface {
	// NewID returns a nonempty opaque identifier.
	NewID() (string, error)
}

// Clock provides deterministic timestamps.
type Clock interface {
	// Now returns the timestamp used for the next lifecycle transition.
	Now() time.Time
}
