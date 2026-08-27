// Package session defines provider-neutral session identity and lifecycle values.
package session

import (
	"errors"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
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

// UserMessage is the provider-neutral user content stored in a session.
type UserMessage = model.Message

// ModelResponse is the provider-neutral terminal model response stored in a session.
type ModelResponse = model.Response

// ToolResult is the provider-neutral terminal tool result stored in a session.
type ToolResult = agent.ToolResult

// Entry is one ordered session record.
type Entry struct {
	// ID uniquely identifies this record within the session.
	ID string
	// CreatedAt determines the record update time without filesystem metadata.
	CreatedAt time.Time
	// Information contains the name change carried by this lifecycle entry.
	Information mo.Option[Information]
	// User contains one terminal user message.
	User mo.Option[UserMessage]
	// Model contains one terminal model response.
	Model mo.Option[ModelResponse]
	// ToolResult contains one terminal tool execution result.
	ToolResult mo.Option[ToolResult]
}

// Replacement is one atomic active-session identity and durable transcript snapshot.
type Replacement struct {
	// Info identifies the committed active session.
	Info Info
	// Entries contains cloned durable entries from the same committed state.
	Entries []Entry
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
