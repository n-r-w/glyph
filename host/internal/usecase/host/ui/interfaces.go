package ui

import (
	"context"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/n-r-w/glyph/internal/operation"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
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
	BindProgress(reporter operation.Reporter[controllerui.Frame]) func()
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
	SelectionCode() string
}

// ModelCatalog supplies configured models and commits runtime selection.
type ModelCatalog interface {
	Models() []model.Descriptor
	ActiveSelection() model.Selection
	SelectModel(ctx context.Context, provider model.ProviderID, modelID model.ID) (model.Selection, error)
	SelectReasoningChoice(choice model.ReasoningChoice) (model.Selection, error)
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
	SignIn(ctx context.Context) error
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
