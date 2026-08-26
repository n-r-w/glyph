// Package session defines provider-neutral session identity and lifecycle values.
package session

import (
	"errors"
	"time"

	"github.com/samber/mo"
)

// ID is the opaque public identifier of one session.
type ID string

var (
	// ErrBusy reports that agent execution or another replacement owns the operation gate.
	ErrBusy = errors.New("another operation is active")
	// ErrInvalidName reports a name that is empty after normalization.
	ErrInvalidName = errors.New("session name is required")
)

// Header is the first record in a persisted session.
type Header struct {
	// Version selects the persisted record schema.
	Version int
	// ID identifies the session independently of its storage path.
	ID ID
	// CreatedAt fixes creation time for ordering and file naming.
	CreatedAt time.Time
	// WorkingDirectory binds the session to one canonical project path.
	WorkingDirectory string
}

// Information is a stored session name change.
type Information struct {
	// Name is the normalized user-assigned session name.
	Name string
}

// Entry is one ordered session record.
type Entry struct {
	// ID uniquely identifies this record within the session.
	ID string
	// CreatedAt determines the record update time without filesystem metadata.
	CreatedAt time.Time
	// Information contains the name change carried by this lifecycle entry.
	Information mo.Option[Information]
}

// Info describes one active or persisted session.
type Info struct {
	// ID is the opaque client-visible session identifier.
	ID ID
	// Name is absent until the user assigns a name.
	Name mo.Option[string]
	// WorkingDirectory is the canonical project path stored in the header.
	WorkingDirectory string
	// StoragePath is absent until the first entry creates the JSONL file.
	StoragePath mo.Option[string]
	// CreatedAt comes from the immutable session header.
	CreatedAt time.Time
	// UpdatedAt is the latest entry time, or CreatedAt for an empty session.
	UpdatedAt time.Time
}

// Summary describes one session in a client list.
type Summary struct {
	// Info contains identity and lifecycle timestamps.
	Info Info
	// FirstUserText is absent when the session contains no user content.
	FirstUserText mo.Option[string]
	// TotalMessages counts client-visible terminal messages.
	TotalMessages int
}
