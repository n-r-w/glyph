//go:build !integration

package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// classifiedModelFailure supplies one model category and complete cause tree in tests.
type classifiedModelFailure struct {
	// code is the provider-neutral model category.
	code string
	// cause contains every acquired failure.
	cause error
}

// Error exposes complete acquired failure text.
func (failure *classifiedModelFailure) Error() string { return failure.cause.Error() }

// ModelCode exposes the provider-neutral model category.
func (failure *classifiedModelFailure) ModelCode() string { return failure.code }

// Unwrap preserves every acquired failure.
func (failure *classifiedModelFailure) Unwrap() error { return failure.cause }

// classifiedContextFailure supplies one context category and complete cause tree in tests.
type classifiedContextFailure struct {
	// code is the context-owner category.
	code string
	// cause contains every acquired failure.
	cause error
}

// Error exposes complete acquired failure text.
func (failure *classifiedContextFailure) Error() string { return failure.cause.Error() }

// ContextCode exposes the context-owner category.
func (failure *classifiedContextFailure) ContextCode() string { return failure.code }

// Unwrap preserves every acquired failure.
func (failure *classifiedContextFailure) Unwrap() error { return failure.cause }

// TestModelFailureMappingClassifiesBeforeCancellation verifies model and context identity precedence.
func TestModelFailureMappingClassifiesBeforeCancellation(t *testing.T) {
	t.Parallel()

	firstTimeout := errors.New("first timeout")
	secondTimeout := errors.New("second timeout")
	independent := errors.New("independent failure")
	for _, test := range []struct {
		name         string
		err          error
		expectedCode string
		causes       []error
	}{
		{
			name: "timeout exhaustion", expectedCode: "RETRY_EXHAUSTED",
			err: &classifiedModelFailure{
				code: "RETRY_EXHAUSTED",
				cause: errors.Join(
					firstTimeout, secondTimeout, context.DeadlineExceeded,
				),
			},
			causes: []error{firstTimeout, secondTimeout, context.DeadlineExceeded},
		},
		{
			name: "mixed model cancellation", expectedCode: "MODEL_FAILED",
			err: &classifiedModelFailure{
				code: "MODEL_FAILED", cause: errors.Join(context.Canceled, independent),
			},
			causes: []error{context.Canceled, independent},
		},
		{
			name: "classified context timeout", expectedCode: "STALE_CONTEXT",
			err: &classifiedContextFailure{
				code: "STALE_CONTEXT", cause: errors.Join(context.DeadlineExceeded, independent),
			},
			causes: []error{context.DeadlineExceeded, independent},
		},
		{
			name: "unclassified mixed cancellation", expectedCode: internalFailureCode,
			err: errors.Join(context.Canceled, independent), causes: []error{context.Canceled, independent},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act through the configured-model controller failure boundary.
			err := mapModelFailure("configured request", test.err)

			// Assert category and every source cause survive the SDK failure wrapper.
			var failure *extensionsdk.FailureError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.expectedCode, failure.Code())
			for _, cause := range test.causes {
				require.ErrorIs(t, err, cause)
			}
		})
	}
}

// TestFailureMappingKeepsOnlyPureUnclassifiedCancellation verifies both controller mapping exits.
func TestFailureMappingKeepsOnlyPureUnclassifiedCancellation(t *testing.T) {
	t.Parallel()

	// Arrange one pure unclassified cancellation for each mapping boundary.
	for name, mapFailure := range map[string]func(string, error) error{
		"model":   mapModelFailure,
		"context": mapContextFailure,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Act without a model or context owner category.
			err := mapFailure("operation", context.Canceled)

			// Assert the true caller cancellation stays unclassified and inspectable.
			var failure *extensionsdk.FailureError
			assert.False(t, errors.As(err, &failure))
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

// TestContextFailureMappingClassifiesBeforeCancellation verifies classified context timeout precedence.
func TestContextFailureMappingClassifiesBeforeCancellation(t *testing.T) {
	t.Parallel()

	// Arrange one context-owned timeout category and one independent cause.
	independent := errors.New("context lookup failed")
	source := &classifiedContextFailure{
		code: "STALE_CONTEXT", cause: errors.Join(context.DeadlineExceeded, independent),
	}

	// Act through the context-operation failure boundary.
	err := mapContextFailure("context request", source)

	// Assert the context category and both causes survive the SDK failure wrapper.
	var failure *extensionsdk.FailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "STALE_CONTEXT", failure.Code())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, independent)
}
