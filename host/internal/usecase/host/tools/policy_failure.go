package tools

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// newPolicyFailureError marks a failed chain as fatal, except a failure whose current cause is only the cancellation
// of the owning context, which Agent Core ends as an abort.
func newPolicyFailureError(ctx context.Context, err error) error {
	if ctx.Err() != nil && errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, ctx.Err()) || errors.Is(cause, context.Cause(ctx))
	}) {
		return err
	}
	return modelexecution.NewFatalExtensionError(err)
}
