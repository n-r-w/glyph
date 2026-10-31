//go:build !integration

package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestRunFailureCodeUsesCurrentExtensionMeaning verifies current and historical category precedence.
func TestRunFailureCodeUsesCurrentExtensionMeaning(t *testing.T) {
	t.Parallel()
	// Arrange one explicit extension category in either current or historical causes.
	cause := errors.New("complete selected extension failure")
	classified := extensionsdk.Fail(controllerui.FailureCodeExtensionFailed, cause)
	current := diagnostic.NewCurrentCause(classified, context.Canceled)
	historical := diagnostic.NewCurrentCause(context.Canceled, classified)

	// Act through the UI accepted-run failure mapper.
	currentCode := runFailureCode(current)
	historicalCode := runFailureCode(historical)

	// Assert current failure retains its category and historical failure cannot replace current cancellation.
	require.Equal(t, controllerui.FailureCodeExtensionFailed, currentCode)
	require.Equal(t, controllerui.FailureCodeInternal, historicalCode)
	require.ErrorIs(t, current, cause)
	require.ErrorIs(t, historical, cause)
}

// TestRunFailureCodeKeepsInputInvalid verifies final-input incompatibility keeps its category and complete text.
func TestRunFailureCodeKeepsInputInvalid(t *testing.T) {
	t.Parallel()
	// Arrange the input-processing failure as run control returns it.
	cause := errors.New("model text does not accept image input at content 1")
	err := fmt.Errorf("process user input: %w", extensionsdk.Fail(controllerui.FailureCodeInputInvalid, cause))

	// Act through the accepted-run failure mapper.
	code := runFailureCode(err)

	// Assert the public category and the complete cause text.
	require.Equal(t, controllerui.FailureCodeInputInvalid, code)
	require.ErrorContains(t, err, cause.Error())
}

// providerFailure is a provider failure with its model-execution category.
type providerFailure struct{}

// Error returns the complete provider failure text.
func (providerFailure) Error() string { return "provider rejected the request" }

// FailureCode returns the model-execution category of the provider failure.
func (providerFailure) FailureCode() string { return controllerui.FailureCodeModelFailed }

// TestRunFailureCodePrefersCurrentFinalizationFailure verifies that a current extension failure determines the
// category when a provider failure is only diagnostic history, and that a provider failure alone keeps its category.
func TestRunFailureCodePrefersCurrentFinalizationFailure(t *testing.T) {
	t.Parallel()
	// Arrange a current finalization failure with the provider failure as diagnostics.
	finalization := extensionsdk.Fail(
		controllerui.FailureCodeExtensionFailed,
		errors.New("finalized-message handler failed"),
	)
	combined := diagnostic.NewCurrentCause(finalization, providerFailure{})

	// Act through the accepted-run failure mapper.
	combinedCode := runFailureCode(combined)
	providerCode := runFailureCode(fmt.Errorf("execute model request: %w", providerFailure{}))

	// Assert the current category and the complete text of both causes.
	require.Equal(t, controllerui.FailureCodeExtensionFailed, combinedCode)
	require.Equal(t, controllerui.FailureCodeModelFailed, providerCode)
	require.ErrorContains(t, combined, "finalized-message handler failed")
	require.ErrorContains(t, combined, providerFailure{}.Error())
}

// TestRunFailureCodeReadsCurrentPersistenceFailure verifies that a persistence failure determines the category only as
// a current cause, and that a current persistence failure keeps its precedence over another current failure.
func TestRunFailureCodeReadsCurrentPersistenceFailure(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		err  error
		code string
	}{
		"diagnostic persistence failure": {
			err: diagnostic.NewCurrentCause(
				extensionsdk.Fail(controllerui.FailureCodeExtensionFailed, errors.New("finalized-message handler failed")),
				fmt.Errorf("commit overflow recovery: %w", agent.ErrPersistenceUnavailable),
			),
			code: controllerui.FailureCodeExtensionFailed,
		},
		"wrapped and joined persistence failure": {
			err: errors.Join(
				fmt.Errorf("append history: %w", fmt.Errorf("write entry: %w", agent.ErrPersistenceUnavailable)), errors.New("other cause"),
			),
			code: controllerui.FailureCodePersistence,
		},
		"persistence failure with another current failure": {
			err: errors.Join(
				extensionsdk.Fail(controllerui.FailureCodeExtensionFailed, errors.New("finalized-message handler failed")),
				fmt.Errorf("append history: %w", agent.ErrPersistenceUnavailable),
			),
			code: controllerui.FailureCodePersistence,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Act through the accepted-run failure mapper.
			code := runFailureCode(test.err)

			// Assert the category of the current causes.
			require.Equal(t, test.code, code)
		})
	}
}
