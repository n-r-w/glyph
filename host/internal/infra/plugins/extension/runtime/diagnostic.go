package runtime

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// inactiveCancellationCode identifies settlement after a cancellation target has stopped.
const inactiveCancellationCode = "TARGET_NOT_ACTIVE"

// connectionDiagnostic selects the original aggregate unless every cause is expected shutdown.
// It never strips independent leaves or reconstructs their text.
func connectionDiagnostic(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if connectionDiagnostic(cause) != nil {
				return err
			}
		}
		return nil
	}
	if isIndependentTerminal(err) {
		return err
	}
	if isExpectedTerminalLeaf(err) {
		return nil
	}
	if cause := errors.Unwrap(err); cause != nil {
		if connectionDiagnostic(cause) == nil {
			return nil
		}
		return err
	}
	// Classify transport leaves only after examining all nested causes.
	code := status.Code(err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) ||
		code == codes.Canceled || code == codes.DeadlineExceeded {
		return nil
	}
	return err
}

// isIndependentTerminal keeps declared failure categories visible even when their cause is cancellation.
func isIndependentTerminal(err error) bool {
	if _, failed := errors.AsType[*extensionsdk.FailureError](err); failed {
		return true
	}
	rejection, rejected := errors.AsType[*extensionsdk.RejectionError](err)
	return rejected && rejection.Code() != inactiveCancellationCode
}

// isExpectedTerminalLeaf recognizes SDK settlement with a plain message, not a nested cause tree.
func isExpectedTerminalLeaf(err error) bool {
	cause := errors.Unwrap(err)
	_, joined := cause.(interface{ Unwrap() []error })
	if joined || errors.Unwrap(cause) != nil {
		return false
	}
	if _, canceled := errors.AsType[*extensionsdk.CanceledError](err); canceled {
		return true
	}
	rejection, rejected := errors.AsType[*extensionsdk.RejectionError](err)
	return rejected && rejection.Code() == inactiveCancellationCode
}
