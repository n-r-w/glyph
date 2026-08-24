package programmatic

import (
	"context"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// Coordinator owns Host run identifiers, execution, and settlement.
type Coordinator interface {
	PrepareRun() (string, error)
	RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error)
}

// Sender delivers transport-independent responses and events synchronously.
type Sender interface {
	SendResponse(ctx context.Context, response controller.Response) error
	SendEvent(ctx context.Context, event controller.AgentEvent) error
}
