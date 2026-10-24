package run

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// classifyToolFailure gives independent operation failure precedence over cleanup cancellation.
func classifyToolFailure(ctx context.Context, execution, progress error) mo.Option[agent.RunOutcome] {
	if _, fatal := errors.AsType[FatalOperationFailure](execution); fatal {
		return mo.Some(agent.RunOutcomeFailed)
	}
	if progress != nil {
		return mo.Some(agent.RunOutcomeFailed)
	}
	if execution != nil && (errors.Is(execution, context.Canceled) ||
		errors.Is(execution, context.DeadlineExceeded) || ctx.Err() != nil) {
		return mo.Some(agent.RunOutcomeAborted)
	}
	return mo.None[agent.RunOutcome]()
}
