package headless

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=headless

// AgentRunner executes one complete Host-coordinated agent run.
type AgentRunner interface {
	Run(ctx context.Context, message model.Message) (agent.RunOutcome, error)
}
