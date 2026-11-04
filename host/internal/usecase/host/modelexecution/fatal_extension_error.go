package modelexecution

import (
	"github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/internal/errtree"
)

// fatalExtensionError is an extension failure that must stop the agent run and keeps the complete failure text and
// identity of its cause, also when the cause is only a cancellation.
type fatalExtensionError struct {
	// cause contains the complete failure.
	cause error
}

var (
	_ extension.FatalOperationFailure = (*fatalExtensionError)(nil)
	_ run.FatalOperationFailure       = (*fatalExtensionError)(nil)
	_ errtree.CodedError              = (*fatalExtensionError)(nil)
)

// NewFatalExtensionError marks the failure of an extension operation as one that stops the owning agent run.
func NewFatalExtensionError(cause error) error {
	return &fatalExtensionError{cause: cause}
}

// Error returns the complete failure text.
func (e *fatalExtensionError) Error() string { return e.cause.Error() }

// Unwrap returns the complete failure.
func (e *fatalExtensionError) Unwrap() error { return e.cause }

// Code returns the public category of an extension failure.
func (e *fatalExtensionError) Code() string { return string(FailureExtensionFailed) }

// FatalOperation requires the owning agent run to stop.
func (e *fatalExtensionError) FatalOperation() {}
