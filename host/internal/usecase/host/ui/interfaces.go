package ui

import (
	"context"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/n-r-w/glyph/internal/operation"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=ui

// Catalog discovers one complete effective UI catalog.
type Catalog interface {
	Discover(ctx context.Context, directory Directory) (Discovery, error)
}

// Runtime starts and closes candidates while retaining the selected process.
type Runtime interface {
	Start(context.Context, Candidate) error
	Close()
}

// Output publishes Host state and binds operation-scoped progress.
type Output interface {
	Initialize(context.Context, Initialization) error
	SetAvailability(Availability) error
	ReportError(code, text string) error
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

// SessionControl provides UI session lifecycle operations.
type SessionControl interface {
	// TryAcquire reserves the shared session-mutation gate for one UI mutation.
	TryAcquire() (func(), bool)
	// Create replaces active state with a new empty session.
	Create(context.Context) (session.Replacement, error)
	// Resume validates and replaces active state by opaque ID.
	Resume(context.Context, session.ID) (session.Replacement, error)
	// SetName persists a normalized active-session name.
	SetName(context.Context, string) (session.Info, error)
	// List returns ordered persisted-session summaries.
	List(context.Context) ([]session.Summary, error)
	// Information returns metadata and statistics from one coherent active-session snapshot.
	Information() session.InformationSnapshot
	// Tree returns the complete active-session tree snapshot.
	Tree() session.Tree
	// Navigate commits one tree navigation with optional built-in summarization.
	Navigate(
		context.Context,
		sessionnavigation.Request,
		func(sessionnavigation.Progress) error,
	) (sessionnavigation.Result, error)
	// Fork creates and activates a replacement before one user message.
	Fork(context.Context, string) (session.Replacement, string, error)
	// Clone creates and activates a copy of the complete active branch.
	Clone(context.Context) (session.Replacement, error)
	// SetLabel persists one entry label and returns the committed tree.
	SetLabel(context.Context, string, string) (session.Tree, error)
}

// Authenticator keeps credential interpretation and refresh inside the provider.
type Authenticator interface {
	CheckAuthentication(ctx context.Context) error
	SignIn(ctx context.Context) error
	IsSignInRequired(err error) bool
}
