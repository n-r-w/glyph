// Package sessions owns the active session and its persisted lifecycle information.
package sessions

import (
	"context"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessions

// Repository stores and loads sessions for one canonical working directory.
type Repository interface {
	// Initialize creates private project storage before clients start.
	Initialize(context.Context) error
	// Apply writes and synchronizes one complete tree mutation.
	Apply(context.Context, ApplyCommand) (ApplyResult, error)
	// CreateSnapshot writes one complete replacement session.
	CreateSnapshot(context.Context, CreateSnapshotCommand) (CreateSnapshotResult, error)
	// List returns files that pass validation for this project.
	List(context.Context) ([]LoadedSession, error)
	// Load resolves an opaque ID through validated headers rather than paths.
	Load(context.Context, session.ID) (LoadedSession, error)
}

// ApplyCommand contains the previous storage path and one mutation to persist.
type ApplyCommand struct {
	// Header is written only when StoragePath is empty.
	Header session.Header
	// StoragePath selects the existing file for later mutations.
	StoragePath string
	// Mutation contains exactly one durable state change.
	Mutation Mutation
}

// ApplyResult contains the path used for a successful mutation.
type ApplyResult struct {
	// StoragePath is the created or reused session file path.
	StoragePath string
}

// Mutation contains exactly one entry, navigation, label, or session-information change.
type Mutation struct {
	// Entry appends one tree entry and makes it active.
	Entry mo.Option[session.Entry]
	// Navigation changes the active leaf and can append one branch summary.
	Navigation mo.Option[NavigationMutation]
	// Label changes one entry label.
	Label mo.Option[LabelMutation]
	// SessionInformation changes the normalized session name.
	SessionInformation mo.Option[SessionInformationMutation]
}

// NavigationMutation contains one active-leaf change and optional embedded summary entry.
type NavigationMutation struct {
	// DestinationID identifies an existing entry or the implicit root when absent.
	DestinationID mo.Option[string]
	// BranchSummary contains the summary entry attached to the destination when present.
	BranchSummary mo.Option[session.Entry]
}

// LabelMutation contains the latest label state for one entry.
type LabelMutation struct {
	// TargetID identifies the labeled tree entry.
	TargetID string
	// Label contains the new label or clears it when empty.
	Label string
}

// SessionInformationMutation contains normalized session metadata.
type SessionInformationMutation struct {
	// Name contains the normalized session name.
	Name string
	// CreatedAt determines the session update timestamp.
	CreatedAt time.Time
}

// CreateSnapshotCommand contains one complete replacement-session snapshot.
type CreateSnapshotCommand struct {
	// Header identifies the new session.
	Header session.Header
	// Tree contains retained entries, labels, and active leaf.
	Tree session.Tree
	// Information contains the retained normalized session name when present.
	Information mo.Option[session.Information]
	// InformationUpdatedAt contains the retained metadata mutation timestamp when present.
	InformationUpdatedAt mo.Option[time.Time]
}

// CreateSnapshotResult contains the created session path.
type CreateSnapshotResult struct {
	// StoragePath is the created session file path.
	StoragePath string
}

// LoadedSession contains one validated session file.
type LoadedSession struct {
	// Header contains the validated session identity and project binding.
	Header session.Header
	// StoragePath is the validated JSONL file path.
	StoragePath string
	// Tree contains validated entries, labels, and the active leaf.
	Tree session.Tree
	// Information contains the latest normalized session metadata.
	Information mo.Option[session.Information]
	// InformationUpdatedAt contains the timestamp of the latest metadata mutation.
	InformationUpdatedAt mo.Option[time.Time]
}

// PricingCatalog resolves configured pricing by exact provider and requested model identity.
type PricingCatalog interface {
	Pricing(model.ProviderID, model.ID) mo.Option[model.Pricing]
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
