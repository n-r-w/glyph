package headless

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=headless

// AgentRunner executes one complete Host-coordinated agent run.
type AgentRunner interface {
	Run(ctx context.Context, userText string) (agent.RunOutcome, error)
}
