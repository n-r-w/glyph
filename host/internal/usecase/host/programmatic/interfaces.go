package programmatic

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// Coordinator owns Host run identifiers, execution, and settlement.
type Coordinator interface {
	PrepareRun() (string, error)
	CancelPrepared(runID string)
	RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error)
}

// SessionControl provides client session lifecycle operations.
type SessionControl interface {
	// Create replaces active state with a new empty session.
	Create(context.Context) (session.Replacement, error)
	// Resume validates and replaces active state by opaque ID.
	Resume(context.Context, session.ID) (session.Replacement, error)
	// SetName persists a normalized active-session name.
	SetName(context.Context, string) (session.Info, error)
	// List returns ordered persisted-session summaries.
	List(context.Context) ([]session.Summary, error)
	// Info returns the current active-session snapshot.
	Info() session.Info
	// Entries returns immutable active-session records.
	Entries() []session.Entry
	// Statistics returns active-session counts and complete token totals.
	Statistics() session.Statistics
	// Tree returns the complete active-session tree snapshot.
	Tree() session.Tree
	// Navigate commits one no-summary tree navigation.
	Navigate(context.Context, string) (sessionnavigation.Result, error)
}

// SelectionCode identifies a model catalog selection failure.
type SelectionCode string

const (
	// SelectionNotFound reports an unknown provider and model pair.
	SelectionNotFound SelectionCode = "not_found"
	// SelectionReasoningUnsupported reports an unsupported reasoning level.
	SelectionReasoningUnsupported SelectionCode = "reasoning_unsupported"
	// SelectionCredentialUnavailable reports unavailable selection credentials.
	SelectionCredentialUnavailable SelectionCode = "credential_unavailable" //nolint:gosec // This is an error code.
)

// SelectionFailure exposes a stable typed catalog failure.
type SelectionFailure interface {
	error
	SelectionCode() string
}

// ModelCatalog provides configured models and runtime selection operations.
type ModelCatalog interface {
	Models() []model.Descriptor
	Selection() model.Selection
	SelectModel(ctx context.Context, provider model.ProviderID, modelID model.ID) (model.Selection, error)
	SelectReasoningChoice(level model.ReasoningChoice) (model.Selection, error)
}
