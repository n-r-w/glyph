//go:build !integration

package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// logicalNavigationFailure supplies one provider-neutral category and its acquired cause in tests.
type logicalNavigationFailure struct {
	// code is the stable logical model-execution category.
	code string
	// cause is the acquired provider, retry, or handler failure.
	cause error
}

// Error exposes the complete acquired failure text.
func (failure *logicalNavigationFailure) Error() string { return failure.cause.Error() }

// FailureCode exposes the provider-neutral logical category.
func (failure *logicalNavigationFailure) FailureCode() string { return failure.code }

// Unwrap preserves the acquired failure cause.
func (failure *logicalNavigationFailure) Unwrap() error { return failure.cause }

// TestSessionOperationFailureCodePrefersLogicalModelCategory verifies the closed retry failure mapping.
func TestSessionOperationFailureCodePrefersLogicalModelCategory(t *testing.T) {
	t.Parallel()

	for _, code := range []string{
		controllerui.FailureCodeRetryExhausted,
		controllerui.FailureCodeRetryCanceled,
		controllerui.FailureCodeExtensionFailed,
		controllerui.FailureCodeRetryDelayExceeded,
		controllerui.FailureCodeContextLimit,
		controllerui.FailureCodeInternal,
	} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			// Arrange the session-tree MODEL_FAILED wrapper with one nested logical failure and source cause.
			cause := errors.New("complete model execution failure")
			logical := &logicalNavigationFailure{code: code, cause: cause}
			err := errors.Join(navigationFailure(t, controllerui.FailureCodeModelFailed), logical)

			// Act through the UI navigation terminal classifier.
			actual := sessionOperationFailureCode(err)

			// Assert the logical category wins while every original error stays available.
			assert.Equal(t, code, actual)
			require.ErrorIs(t, err, cause)
			var retained *logicalNavigationFailure
			require.ErrorAs(t, err, &retained)
			assert.Same(t, logical, retained)
		})
	}
}
