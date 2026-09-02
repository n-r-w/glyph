package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
)

// transportError preserves a local cause with one gRPC status category.
type transportError struct {
	// code is the public gRPC status category.
	code codes.Code
	// cause preserves the complete source cause.
	cause error
}

// Error returns complete transport failure text.
func (e *transportError) Error() string { return e.cause.Error() }

// Unwrap returns the source transport cause.
func (e *transportError) Unwrap() error { return e.cause }

// GRPCStatus exposes the classified status without replacing the source cause.
func (e *transportError) GRPCStatus() *status.Status { return status.New(e.code, e.cause.Error()) }

// classifyTransportError preserves incoming status and classifies queue overflow.
func classifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, operation.ErrQueueFull) {
		return &transportError{code: codes.ResourceExhausted, cause: err}
	}
	if containsOnly(err, context.Canceled) || containsOnly(err, io.EOF) {
		return err
	}
	if _, ok := errors.AsType[*operationError](err); ok {
		return err
	}
	return &transportError{code: codes.Unavailable, cause: err}
}

// containsOnly reports whether every joined or wrapped leaf matches the expected error.
func containsOnly(err, expected error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !containsOnly(cause, expected) {
				return false
			}
		}
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return containsOnly(cause, expected)
	}
	return errors.Is(err, expected)
}

// withoutTransportClosureLeaves removes only pure cancellation and EOF leaves from joined errors.
func withoutTransportClosureLeaves(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		remaining := make([]error, 0, len(joined.Unwrap()))
		for _, cause := range joined.Unwrap() {
			if filtered := withoutTransportClosureLeaves(cause); filtered != nil {
				remaining = append(remaining, filtered)
			}
		}
		return errors.Join(remaining...)
	}
	cause := errors.Unwrap(err)
	if cause != nil {
		if !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) &&
			!errors.Is(cause, io.EOF) {
			return err
		}
		filtered := withoutTransportClosureLeaves(cause)
		if filtered == nil {
			return nil
		}
		return fmt.Errorf("%s: %w", err.Error(), filtered)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// operationError preserves one classified UI contract failure.
type operationError struct {
	// code is the stable machine-readable category.
	code string
	// cause preserves complete error text.
	cause error
}

// Error returns complete public error text.
func (e *operationError) Error() string { return e.cause.Error() }

// Code returns the stable machine-readable category.
func (e *operationError) Code() string { return e.code }

// Unwrap returns the preserved contract cause.
func (e *operationError) Unwrap() error { return e.cause }

// newOperationError creates one classified contract error.
func newOperationError(code, message string) error {
	return &operationError{code: code, cause: errors.New(message)}
}
