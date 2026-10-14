//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/errtree"
)

// TestCurrentCancellationRetainsRecoveredDiagnostics checks terminal disposition across wrappers and siblings.
func TestCurrentCancellationRetainsRecoveredDiagnostics(t *testing.T) {
	t.Parallel()
	// Arrange recovered attempt diagnostics and current terminal causes.
	recovered := errors.New("recovered provider cause")
	history := &ProviderFailureError{
		Classification: ProviderFailureTransient,
		RetryDelay:     mo.None[time.Duration](),
		Cause:          recovered,
	}
	boundary := diagnostic.NewCurrentCause(context.Canceled, history)
	independent := errors.New("independent terminal cause")
	classified := &LogicalFailureError{Category: FailureExtensionFailed, Cause: context.Canceled}
	cases := []struct {
		name     string
		cause    error
		canceled bool
		plain    bool
	}{
		{name: "plain cancellation", cause: context.Canceled, canceled: true, plain: true},
		{name: "recovered provider", cause: boundary, canceled: true, plain: false},
		{
			name:     "ordinary wrapper",
			cause:    fmt.Errorf("terminal observation: %w", boundary),
			canceled: true,
			plain:    false,
		},
		{
			name: "cancellation diagnostic history",
			cause: diagnostic.NewCurrentCause(
				context.Canceled,
				fmt.Errorf("prior canceled attempt: %w", context.Canceled),
			),
			canceled: true,
			plain:    false,
		},
		{name: "independent after", cause: errors.Join(boundary, independent), canceled: false, plain: false},
		{name: "independent before", cause: errors.Join(independent, boundary), canceled: false, plain: false},
		{
			name:     "classified enclosing",
			cause:    &LogicalFailureError{Category: FailureExtensionFailed, Cause: boundary},
			canceled: false,
			plain:    false,
		},
		{name: "classified sibling after", cause: errors.Join(boundary, classified), canceled: false, plain: false},
		{name: "classified sibling before", cause: errors.Join(classified, boundary), canceled: false, plain: false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Act through shared current-cause and plain-text decisions.
			match := func(cause error) bool { return errors.Is(cause, context.Canceled) }
			canceled := errtree.IsCurrentCancellation(scenario.cause, match)
			plain := errtree.IsPlainCancellation(scenario.cause, match)
			// Assert current-cause classification and whether callers need full diagnostic text.
			require.Equal(t, scenario.canceled, canceled)
			require.Equal(t, scenario.plain, plain)
		})
	}
	// Assert ordinary error inspection still reaches every original cause and typed source.
	require.ErrorIs(t, boundary, context.Canceled)
	require.ErrorIs(t, boundary, recovered)
	actual, present := errors.AsType[*ProviderFailureError](boundary)
	require.True(t, present)
	require.Same(t, history, actual)
	require.EqualError(t, boundary, errors.Join(context.Canceled, history).Error())
}
