package runtime

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// classifyCompactionFailure preserves a received canceled terminal before recording independent failure meaning.
func classifyCompactionFailure(ctx context.Context, cause error) error {
	if isReceivedCancellation(cause) {
		return &extensioncontext.PolicyCancellationError{Cause: cause}
	}
	return extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, cause)
}

// isReceivedCancellation checks explicit remote cancellation without promoting historical or mixed sources.
func isReceivedCancellation(cause error) bool {
	received := false
	for current := range errtree.IterateCurrentErrors(cause) {
		//nolint:errorlint // Current-node inspection excludes canceled terminals retained only as diagnostics.
		if _, present := current.(*extensionsdk.CanceledError); present {
			received = true
		}
	}
	return received && errtree.IsCurrentCancellation(cause, func(current error) bool {
		return errors.Is(current, context.Canceled) || errors.Is(current, context.DeadlineExceeded)
	})
}
