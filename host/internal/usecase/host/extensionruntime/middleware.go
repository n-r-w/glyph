package extensionruntime

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
)

// HandleWire invokes mechanics only for the exact generation bound by the middleware owner.
func (s *Service) HandleWire(
	ctx context.Context,
	extensionID, handlerID string,
	invocation middleware.Invocation,
) (middleware.Action, error) {
	owner, available := s.beginOperation(extensionID, invocation.Context.RuntimeInstanceID)
	if !available {
		return middleware.Action{}, fmt.Errorf(
			"%w: middleware handler %q is unavailable",
			ErrExtensionUnavailable,
			handlerID,
		)
	}
	action, err := owner.state.runtime.HandleWire(ctx, handlerID, invocation)
	s.finishAndReport(ctx, owner, err)
	return action, err
}
