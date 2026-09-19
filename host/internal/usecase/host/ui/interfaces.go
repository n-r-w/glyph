package ui

import (
	"context"
	"time"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/n-r-w/glyph/internal/operation"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/authentication"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=ui

// Catalog discovers one complete effective UI catalog.
type Catalog interface {
	Discover(ctx context.Context, directory Directory) (Discovery, error)
}

// Runtime starts and closes candidates while retaining the selected process.
type Runtime interface {
	Start(context.Context, Candidate) error
	Close() error
}

// Output publishes Host state and binds operation-scoped progress.
type Output interface {
	Initialize(context.Context, Initialization) error
	SetAvailability(Availability) error
	ReportError(code string, cause error) error
	BindProgress(runID string, reporter operation.Reporter[controllerui.Frame]) func()
}

// RetryControl owns runtime retry enablement and one atomic policy projection.
type RetryControl interface {
	// SetRetryEnabled changes enablement for logical executions that start later.
	SetRetryEnabled(enabled bool)
	// RetryPolicy returns one detached effective policy snapshot.
	RetryPolicy() (enabled bool, maxRetries int64, delays []time.Duration, maxProviderDelay time.Duration)
}

// AgentRunner starts one user request against the retained Agent Core history.
type AgentRunner interface {
	PrepareRun() (string, error)
	CancelPrepared(runID string)
	RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error)
}

// SelectionFailure preserves the classified cause returned by model selection.
type SelectionFailure interface {
	error
	ModelSelectionCode() string
}

// ModelCatalog supplies configured models and the active selection snapshot.
type ModelCatalog interface {
	Models() []model.Descriptor
	ActiveSelection() model.Selection
}

// ModelSelectionCommandKind identifies one UI selection request shape.
type ModelSelectionCommandKind uint8

const (
	// ModelSelectionCommandModel requests one provider and model target.
	ModelSelectionCommandModel ModelSelectionCommandKind = iota + 1
	// ModelSelectionCommandReasoning requests one reasoning choice for the active model.
	ModelSelectionCommandReasoning
)

// ModelSelectionCommand contains one validated UI selection request.
type ModelSelectionCommand struct {
	// Kind identifies the selected request shape.
	Kind ModelSelectionCommandKind
	// Provider identifies the requested provider for a model request.
	Provider model.ProviderID
	// Model identifies the requested model for a model request.
	Model model.ID
	// ReasoningChoice identifies the requested reasoning choice for a reasoning request.
	ReasoningChoice model.ReasoningChoice
}

// ModelSelectionResult contains one shared selection execution outcome projected for UI.
type ModelSelectionResult struct {
	// Selection is the committed selection when Committed is true.
	Selection model.Selection
	// Committed reports whether authoritative selection state was committed.
	Committed bool
	// Issues contains ordered public diagnostics for a committed result.
	Issues []ModelSelectionIssue
	// Source preserves complete diagnostic or terminal failure causes.
	Source error
}

// PreparedModelSelection owns one admitted UI selection until release.
type PreparedModelSelection interface {
	// Run executes handler composition, final validation, commit, and publication.
	Run(context.Context) ModelSelectionResult
	// Release frees shared selection admission exactly once.
	Release()
}

// ModelSelection prepares all UI selection changes through the shared Host owner.
type ModelSelection interface {
	// PrepareUISelection validates and reserves one consumer-owned UI selection command.
	PrepareUISelection(ModelSelectionCommand) (PreparedModelSelection, error)
}

// ActiveSessions provides UI session lifecycle operations.
type ActiveSessions interface {
	// CreateActive replaces active state with a new empty session.
	CreateActive() (session.Info, []session.Entry, error)
	// ResumeActive validates and replaces active state by opaque ID.
	ResumeActive(context.Context, session.ID) (session.Info, []session.Entry, error)
	// SetActiveName persists a normalized active-session name.
	SetActiveName(context.Context, string) (session.Info, error)
	// ListUISessions returns ordered persisted-session summaries.
	ListUISessions(context.Context) ([]StoredSession, error)
	// ActiveInformation returns metadata and statistics from one coherent active-session snapshot.
	ActiveInformation() (session.Info, session.Statistics)
	// Tree returns the complete active-session tree snapshot.
	Tree() session.Tree
	// ForkActive creates and activates a replacement before one user message.
	ForkActive(context.Context, string) (session.Info, []session.Entry, string, error)
	// CloneActive creates and activates a copy of the complete active branch.
	CloneActive(context.Context) (session.Info, []session.Entry, error)
	// SetLabel persists one entry label and returns the committed tree.
	SetLabel(context.Context, string, string) (session.Tree, error)
}

// Authenticator keeps credential interpretation and refresh inside the provider.
type Authenticator interface {
	CheckAuthentication(ctx context.Context) error
	SignIn(ctx context.Context, method authentication.Method) error
	IsSignInRequired(err error) bool
}

// Gate reserves session mutations before operation acceptance.
type Gate interface {
	// TryAcquire returns a release function when the shared reservation is available.
	TryAcquire() (func(), bool)
}

// RuntimeActivation starts accepted extension monitoring after initialized output is connected.
type RuntimeActivation interface {
	// Activate starts asynchronous runtime observation without waiting for authentication.
	Activate(context.Context)
	// StopReporting joins admitted reporter calls without stopping runtime monitoring.
	StopReporting()
}
