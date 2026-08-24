package programmatic

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// Coordinator owns Host run identifiers, execution, and settlement.
type Coordinator interface {
	PrepareRun() (string, error)
	RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error)
}
