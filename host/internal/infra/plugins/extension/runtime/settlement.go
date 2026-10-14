package runtime

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/internal/errtree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// targetNotActiveCode identifies rejection of an inactive cancellation target.
const targetNotActiveCode = "TARGET_NOT_ACTIVE"

// operationTerminal holds the original operation's terminal payload or error.
type operationTerminal struct {
	// completed contains the successful wire payload.
	completed *extensionpb.ExtensionCompleted
	// cause contains the terminal or connection error.
	cause error
}

// waitForOperation settles an invocation on its original operation handle and joins any cancellation work.
func (r *Runtime) waitForOperation(
	ctx context.Context,
	id string,
	started *extensionsdk.Operation,
	progress func(*extensionpb.ToolProgress) error,
) (*extensionpb.ExtensionCompleted, error) {
	var callbackErr error
	var callback func(*extensionpb.ToolProgress) error
	if progress != nil {
		callback = func(payload *extensionpb.ToolProgress) error {
			callbackErr = progress(payload)
			return callbackErr
		}
	}
	completed, initialErr := started.Wait(ctx, callback)
	if completed != nil || callbackErr == nil && isExtensionTerminalError(initialErr) {
		return completed, initialErr
	}
	if callbackErr == nil && isConnectionFailure(initialErr) {
		if status.Code(initialErr) == codes.FailedPrecondition {
			return nil, r.protocolViolation(initialErr)
		}
		return nil, errors.Join(extensionruntime.ErrExtensionUnavailable, retainReceivedCancellation(initialErr))
	}
	// The first Wait has returned before this goroutine becomes the original channel's sole reader.
	settled := make(chan operationTerminal, 1)
	cleanupCtx := context.WithoutCancel(ctx)
	go func() {
		payload, cause := started.Wait(cleanupCtx, nil)
		settled <- operationTerminal{completed: payload, cause: cause}
	}()
	cancellationErr := r.cancelOperation(cleanupCtx, id)
	original := <-settled
	unavailable := isConnectionFailure(original.cause) || isConnectionFailure(cancellationErr)
	originalTerminal := original.completed != nil || isExtensionTerminalError(original.cause)
	var diagnostics error
	if originalTerminal && isInactiveTargetRejection(cancellationErr) {
		diagnostics = cancellationErr
		cancellationErr = nil
	}
	originalCause := original.cause
	if isConnectionFailure(originalCause) {
		originalCause = retainReceivedCancellation(originalCause)
	}
	current := errors.Join(initialErr, originalCause, cancellationErr)
	if unavailable {
		current = errors.Join(extensionruntime.ErrExtensionUnavailable, current)
	}
	return original.completed, diagnostic.NewCurrentCause(current, diagnostics)
}

// isInactiveTargetRejection identifies cancellation rejections for inactive targets.
func isInactiveTargetRejection(err error) bool {
	if err == nil {
		return false
	}
	//nolint:errorlint // Only the rejection at this current node corroborates original settlement.
	if rejection, ok := err.(*extensionsdk.RejectionError); ok {
		return rejection.Code() == targetNotActiveCode
	}
	//nolint:errorlint // Diagnostic history does not establish a current cancellation rejection.
	if boundary, ok := err.(errtree.CurrentCause); ok {
		return isInactiveTargetRejection(boundary.CurrentCause())
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isInactiveTargetRejection(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return isInactiveTargetRejection(wrapped.Unwrap())
	}
	return false
}
