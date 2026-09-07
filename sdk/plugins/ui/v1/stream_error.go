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

// streamError exposes the selected gRPC code without replacing the local cause tree.
type streamError struct {
	// code is selected from delivery and protocol failures before adding report sources.
	code codes.Code
	// cause retains all independent completion errors.
	cause error
}

// Error returns the standard gRPC rendering of the complete cause.
func (e *streamError) Error() string { return e.GRPCStatus().Err().Error() }

// GRPCStatus exposes the selected wire status and full diagnostic.
func (e *streamError) GRPCStatus() *status.Status { return status.New(e.code, e.cause.Error()) }

// Unwrap retains local error identities for SDK completion consumers.
func (e *streamError) Unwrap() error { return e.cause }

// joinOutputSources adds report snapshots only after classifying delivery failures.
func joinOutputSources(delivery error, sources ...error) error {
	classified := streamStatus(delivery)
	source := errors.Join(sources...)
	if source == nil {
		return classified
	}
	code := status.Code(classified)
	if code == codes.OK {
		code = codes.Unavailable
	}
	return &streamError{code: code, cause: errors.Join(delivery, source)}
}

// pendingReceiveError collects an application failure without filtering cancellation from its source.
func pendingReceiveError(received <-chan error) error {
	select {
	case err := <-received:
		if withoutClosureLeaves(err) != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

// streamStatus preserves transport status and classifies local stream failures.
func streamStatus(err error) error {
	if err == nil {
		return nil
	}
	if grpcStatus, ok := status.FromError(err); ok {
		return &streamError{code: grpcStatus.Code(), cause: err}
	}
	if _, ok := errors.AsType[*protocolError](err); ok {
		return &streamError{code: codes.FailedPrecondition, cause: err}
	}
	if errors.Is(err, operation.ErrQueueFull) {
		return &streamError{code: codes.ResourceExhausted, cause: err}
	}
	return &streamError{code: codes.Unavailable, cause: err}
}
