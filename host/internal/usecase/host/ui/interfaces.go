package ui

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
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
}

// AgentRunner starts one user request against the retained Agent Core history.
type AgentRunner interface {
	Run(ctx context.Context, userText string) (agent.RunOutcome, error)
}

// Authenticator keeps credential interpretation and refresh inside the provider.
type Authenticator interface {
	CheckAuthentication(ctx context.Context) error
	SignIn(ctx context.Context) error
	IsSignInRequired(err error) bool
}
