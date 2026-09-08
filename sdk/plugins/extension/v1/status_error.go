package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"io"

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

// withoutClosureLeaves removes transport shutdown leaves before report sources are added.
func withoutClosureLeaves(err error) error {
	if err == nil ||
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, io.EOF) {
		return err
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var remaining []error
		for _, cause := range joined.Unwrap() {
			remaining = append(remaining, withoutClosureLeaves(cause))
		}
		return errors.Join(remaining...)
	}
	if cause := errors.Unwrap(err); cause != nil {
		if remaining := withoutClosureLeaves(cause); remaining != nil {
			return fmt.Errorf("%s: %w", err.Error(), remaining)
		}
	}
	return nil
}

// isTransportClosureOnly identifies expected transport shutdown without inspecting declared source snapshots.
// Mixed trees remain independent failures and retain their original values.
func isTransportClosureOnly(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !isTransportClosureOnly(cause) {
				return false
			}
		}
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return isTransportClosureOnly(cause)
	}
	code := status.Code(err)
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) ||
		code == codes.Canceled || code == codes.DeadlineExceeded
}

// joinOutputSources adds source-only snapshots after delivery classification and cleanup filtering.
func joinOutputSources(delivery error, sources ...error) error {
	source := errors.Join(sources...)
	if source == nil {
		return delivery
	}
	code := status.Code(mapDeliveryError(delivery))
	if code == codes.OK {
		code = codes.Unavailable
	}
	combined := errors.Join(delivery, source)
	return newCausedStatusError(code, combined.Error(), combined)
}

// newProtocolStatusError classifies a local protocol, decode, or mapping cause.
func newProtocolStatusError(code codes.Code, message string, cause error) error {
	return newCausedStatusError(code, message, cause)
}
