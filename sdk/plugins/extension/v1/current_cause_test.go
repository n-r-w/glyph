//go:build !integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestOperationOutcomeCurrentCauses checks that the SDK prioritizes current failures
// and retains cancellation sources with diagnostics.
func TestOperationOutcomeCurrentCauses(t *testing.T) {
	t.Parallel()
	// Arrange current cancellation with a recovered typed failure.
	prior := Fail(failureCodeInternal, errors.New("prior failure diagnostic"))
	rich := diagnostic.NewCurrentCause(context.Canceled, prior)
	const activeFailureCode = "TOOL_FAILED"
	active := Fail(activeFailureCode, context.Canceled)
	rejected := Reject(rejectionCodeTargetNotActive, context.Canceled)
	nested := &contextOperationError{id: "nested", code: failureCodeInternal, cause: errors.New("nested failure")}
	for _, scenario := range []struct {
		name   string
		cause  error
		state  operation.TerminalState
		source bool
	}{
		{name: "rich cancellation", cause: rich, state: operation.TerminalStateCanceled, source: true},
		{name: "wrapped rich cancellation", cause: fmt.Errorf("outer: %w", rich), state: operation.TerminalStateCanceled, source: true},
		{name: "plain cancellation", cause: context.Canceled, state: operation.TerminalStateCanceled, source: false},
		{name: "active failure", cause: active, state: operation.TerminalStateFailed, source: true},
		{name: "active rejection", cause: rejected, state: operation.TerminalStateFailed, source: true},
		{name: "failure before history", cause: errors.Join(active, rich), state: operation.TerminalStateFailed, source: true},
		{name: "failure after history", cause: errors.Join(rich, active), state: operation.TerminalStateFailed, source: true},
		{name: "nested before failure", cause: errors.Join(nested, active), state: operation.TerminalStateFailed, source: true},
		{name: "nested after failure", cause: errors.Join(active, nested), state: operation.TerminalStateFailed, source: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Act through the SDK outcome classifier and the shared cancellation predicate.
			outcome := operationOutcome[struct{}](scenario.cause)
			canceled := errtree.IsCurrentCancellation(
				scenario.cause,
				func(leaf error) bool { return errors.Is(leaf, context.Canceled) },
			)
			// Assert that the SDK keeps active failures and the original diagnostic source.
			require.Equal(t, scenario.state, outcome.State())
			require.Equal(t, scenario.state == operation.TerminalStateCanceled, canceled)
			if scenario.source {
				require.Same(t, scenario.cause, outcome.SourceError())
				require.Contains(t, outcome.SourceError().Error(), scenario.cause.Error())
			} else {
				require.Nil(t, outcome.SourceError())
			}
			if errors.Is(scenario.cause, nested) {
				require.Equal(t, failureCodeInternal, outcome.Code())
			}
		})
	}
}
