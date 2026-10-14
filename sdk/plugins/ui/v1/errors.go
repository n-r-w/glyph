package uiv1

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/n-r-w/glyph/internal/errtree"
)

// RejectionError reports a request that did not create an operation.
type RejectionError struct {
	// code is the stable machine-readable rejection category.
	code string
	// cause preserves the complete rejection cause.
	cause error
}

// The current-cause classifier recognizes this rejection's category.
var _ errtree.CodedError = (*RejectionError)(nil)

// Error returns the complete rejection text.
func (e *RejectionError) Error() string { return e.cause.Error() }

// Code returns the stable machine-readable rejection category.
func (e *RejectionError) Code() string { return e.code }

// Unwrap returns the original rejection cause.
func (e *RejectionError) Unwrap() error { return e.cause }

// Reject constructs a classified preparation rejection.
func Reject(code string, cause error) error {
	if code == "" {
		panic("UI rejection code is required")
	}
	if cause == nil {
		panic("UI rejection cause is required")
	}
	return &RejectionError{code: code, cause: cause}
}

// FailureError reports classified accepted-operation failure.
type FailureError struct {
	// code is the stable machine-readable failure category.
	code string
	// cause preserves the complete failure cause.
	cause error
}

// The current-cause classifier recognizes this failure's category.
var _ errtree.CodedError = (*FailureError)(nil)

// Error returns the complete failure text.
func (e *FailureError) Error() string { return e.cause.Error() }

// Code returns the stable machine-readable failure category.
func (e *FailureError) Code() string { return e.code }

// Unwrap returns the original failure cause.
func (e *FailureError) Unwrap() error { return e.cause }

// Fail constructs a classified accepted-operation failure.
func Fail(code string, cause error) error {
	if code == "" {
		panic("UI failure code is required")
	}
	if cause == nil {
		panic("UI failure cause is required")
	}
	return &FailureError{code: code, cause: cause}
}

// CanceledError reports an operation canceled after acceptance.
type CanceledError struct {
	// cause preserves the cancellation cause.
	cause error
}

// Error returns the complete cancellation text.
func (e *CanceledError) Error() string { return e.cause.Error() }

// Unwrap returns the cancellation cause.
func (e *CanceledError) Unwrap() error { return e.cause }

// newRemoteRejection reconstructs the public SDK rejection surface.
func newRemoteRejection(code, message string) error {
	return &RejectionError{code: code, cause: errors.New(message)}
}

// newRemoteFailure reconstructs the public SDK failure surface.
func newRemoteFailure(code, message string) error {
	return &FailureError{code: code, cause: errors.New(message)}
}

// newCanceledError constructs the public cancellation result.
func newCanceledError(message string) error {
	cause := fmt.Errorf("UI operation canceled: %w", context.Canceled)
	if message != "" {
		return &CanceledError{cause: diagnostic.NewCurrentCause(cause, errors.New(message))}
	}
	return &CanceledError{cause: cause}
}
