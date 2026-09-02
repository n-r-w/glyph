package uiv1

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
)

// protocolError marks one peer contract violation.
type protocolError struct {
	// cause preserves the complete protocol fault.
	cause error
}

// Error returns complete protocol fault text.
func (e *protocolError) Error() string { return e.cause.Error() }

// Unwrap returns the protocol fault cause.
func (e *protocolError) Unwrap() error { return e.cause }

// protocolFault classifies one peer contract violation.
func protocolFault(err error) error {
	if err == nil || errors.Is(err, operation.ErrQueueFull) {
		return err
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return &protocolError{cause: err}
}

// streamStatus preserves transport status and classifies local stream failures.
func streamStatus(err error) error {
	if err == nil {
		return nil
	}
	if grpcStatus, ok := status.FromError(err); ok {
		return status.Error(grpcStatus.Code(), err.Error())
	}
	if _, ok := errors.AsType[*protocolError](err); ok {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	if errors.Is(err, operation.ErrQueueFull) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	return status.Error(codes.Unavailable, err.Error())
}
