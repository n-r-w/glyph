package tools

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// policyFailureError is a failure of the tool-policy chain that must stop the agent run, because the call did not
// pass the policy and the tool did not run.
type policyFailureError struct {
	// cause contains the complete failure of the chain.
	cause error
}

var (
	_ run.FatalOperationFailure = (*policyFailureError)(nil)
	_ errtree.CodedError        = (*policyFailureError)(nil)
)

// Error returns the complete failure text.
func (e *policyFailureError) Error() string { return e.cause.Error() }

// Unwrap returns the complete failure.
func (e *policyFailureError) Unwrap() error { return e.cause }

// Code returns the public category of an extension failure.
func (e *policyFailureError) Code() string { return string(modelexecution.FailureExtensionFailed) }

// FatalOperation requires the owning agent run to stop.
func (e *policyFailureError) FatalOperation() {}

// newPolicyFailureError marks a failed chain as fatal, except a failure whose current cause is only the cancellation
// of the owning context, which Agent Core ends as an abort.
func newPolicyFailureError(ctx context.Context, err error) error {
	if ctx.Err() != nil && errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, ctx.Err()) || errors.Is(cause, context.Cause(ctx))
	}) {
		return err
	}
	return &policyFailureError{cause: err}
}
