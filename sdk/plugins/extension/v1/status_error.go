package extensionv1

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
)

// causedStatusError preserves a Go cause while exposing an intended gRPC status.
type causedStatusError struct {
	// status contains the public gRPC code and complete message.
	status *status.Status
	// cause is the original classified error.
	cause error
}

// Error returns the standard gRPC error text.
func (e *causedStatusError) Error() string { return e.status.Err().Error() }

// GRPCStatus returns the public status used by gRPC.
func (e *causedStatusError) GRPCStatus() *status.Status { return e.status }

// Unwrap returns the original classified error.
func (e *causedStatusError) Unwrap() error { return e.cause }

// newCausedStatusError constructs a status error without removing its source cause.
func newCausedStatusError(code codes.Code, message string, cause error) error {
	return &causedStatusError{status: status.New(code, message), cause: cause}
}

// mapDeliveryError maps bounded queue overflow and other transport failures.
func mapDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	if _, present := status.FromError(err); present {
		return err
	}
	if errors.Is(err, operation.ErrQueueFull) {
		return newCausedStatusError(
			codes.ResourceExhausted,
			fmt.Sprintf("extension delivery queue is full: %v", err),
			err,
		)
	}
	return newCausedStatusError(codes.Unavailable, fmt.Sprintf("extension transport failed: %v", err), err)
}

// newProtocolStatusError classifies a local protocol, decode, or mapping cause.
func newProtocolStatusError(code codes.Code, message string, cause error) error {
	return newCausedStatusError(code, message, cause)
}
