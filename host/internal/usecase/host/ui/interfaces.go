package ui

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=ui

// Catalog discovers one complete effective UI catalog.
type Catalog interface {
	Discover(ctx context.Context, directory domainui.Directory) (domainui.Discovery, error)
}

// RuntimeFactory starts one UI candidate and validates fixed startup capabilities.
type RuntimeFactory interface {
	Start(ctx context.Context, candidate domainui.Candidate) (Runtime, error)
}

// Runtime owns one connected UI process.
type Runtime interface {
	Capabilities() domainui.Capabilities
	Open(ctx context.Context) (Channel, error)
	Close()
}

// Channel carries provider-neutral Host frames and UI commands on one stream.
type Channel interface {
	Send(frame domainui.Frame) error
	Receive() (domainui.Command, error)
	Close()
}

// AgentRunner starts one user request against the retained Agent Core history.
type AgentRunner interface {
	Run(ctx context.Context, userText string) (agent.RunOutcome, error)
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
	Navigate(context.Context, sessionnavigation.Request) (sessionnavigation.Result, error)
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
