//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestRunFailureCodeUsesCurrentExtensionMeaning verifies retained failure categories do not replace current cancellation.
func TestRunFailureCodeUsesCurrentExtensionMeaning(t *testing.T) {
	t.Parallel()
	// Arrange an explicit extension category and separate current and historical error branches.
	cause := errors.New("complete selected extension failure")
	classified := extensionsdk.Fail(controller.FailureCodeExtensionFailed, cause)
	current := diagnostic.NewCurrentCause(classified, context.Canceled)
	historical := diagnostic.NewCurrentCause(context.Canceled, classified)

	// Act through the Programmatic accepted-run failure mapper.
	currentCode := failureCode(current)
	historicalCode := failureCode(historical)

	// Assert current extension failure retains its category while diagnostic history alone has no such authority.
	require.Equal(t, controller.FailureCodeExtensionFailed, currentCode)
	require.Equal(t, controller.FailureCodeInternal, historicalCode)
	require.ErrorIs(t, current, cause)
	require.ErrorIs(t, historical, cause)
}

// TestRunFailureCodeKeepsInputInvalid verifies final-input incompatibility keeps its category and complete text.
func TestRunFailureCodeKeepsInputInvalid(t *testing.T) {
	t.Parallel()
	// Arrange the input-processing failure as run control returns it.
	cause := errors.New("model text does not accept image input at content 1")
	err := fmt.Errorf("process user input: %w", extensionsdk.Fail(controller.FailureCodeInputInvalid, cause))

	// Act through the accepted-run failure mapper.
	code := failureCode(err)

	// Assert the public category and the complete cause text.
	require.Equal(t, controller.FailureCodeInputInvalid, code)
	require.ErrorContains(t, err, cause.Error())
}

// providerFailure is a provider failure with its model-execution category.
type providerFailure struct{}

// Error returns the complete provider failure text.
func (providerFailure) Error() string { return "provider rejected the request" }

// FailureCode returns the model-execution category of the provider failure.
func (providerFailure) FailureCode() string { return controller.FailureCodeModelFailed }

// TestRunFailureCodePrefersCurrentFinalizationFailure verifies that a current extension failure determines the
// category when a provider failure is only diagnostic history, and that a provider failure alone keeps its category.
func TestRunFailureCodePrefersCurrentFinalizationFailure(t *testing.T) {
	t.Parallel()
	// Arrange a current finalization failure with the provider failure as diagnostics.
	finalization := extensionsdk.Fail(
		controller.FailureCodeExtensionFailed,
		errors.New("finalized-message handler failed"),
	)
	combined := diagnostic.NewCurrentCause(finalization, providerFailure{})

	// Act through the accepted-run failure mapper.
	combinedCode := failureCode(combined)
	providerCode := failureCode(fmt.Errorf("execute model request: %w", providerFailure{}))

	// Assert the current category and the complete text of both causes.
	require.Equal(t, controller.FailureCodeExtensionFailed, combinedCode)
	require.Equal(t, controller.FailureCodeModelFailed, providerCode)
	require.ErrorContains(t, combined, "finalized-message handler failed")
	require.ErrorContains(t, combined, providerFailure{}.Error())
}

// compactionFailure is a compaction failure with its compaction category.
type compactionFailure struct{}

// Error returns the complete compaction failure text.
func (compactionFailure) Error() string { return "compaction policy canceled overflow recovery" }

// CompactionFailureCode returns the compaction category.
func (compactionFailure) CompactionFailureCode() string {
	return controller.FailureCodeCompactionFailed
}

// TestRunFailureCodeIgnoresDiagnosticCompactionFailure verifies that a current extension failure takes precedence
// over a compaction failure kept as diagnostic history, while a current compaction failure keeps its category.
func TestRunFailureCodeIgnoresDiagnosticCompactionFailure(t *testing.T) {
	t.Parallel()
	// Arrange a current finalization failure with the compaction failure as diagnostics.
	finalization := extensionsdk.Fail(
		controller.FailureCodeExtensionFailed,
		errors.New("finalized-message handler failed"),
	)
	combined := diagnostic.NewCurrentCause(finalization, compactionFailure{})

	// Act through the accepted-run failure mapper.
	combinedCode := failureCode(combined)
	compactionCode := failureCode(fmt.Errorf("recover context overflow: %w", compactionFailure{}))

	// Assert the current category and the complete text of both causes.
	require.Equal(t, controller.FailureCodeExtensionFailed, combinedCode)
	require.Equal(t, controller.FailureCodeCompactionFailed, compactionCode)
	require.ErrorContains(t, combined, compactionFailure{}.Error())
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
				extensionsdk.Fail(controller.FailureCodeExtensionFailed, errors.New("finalized-message handler failed")),
				fmt.Errorf("commit overflow recovery: %w", session.ErrPersistenceUnavailable),
			),
			code: controller.FailureCodeExtensionFailed,
		},
		"wrapped and joined persistence failure": {
			err: errors.Join(
				fmt.Errorf("append history: %w", fmt.Errorf("write entry: %w", session.ErrPersistenceUnavailable)), errors.New("other cause"),
			),
			code: controller.FailureCodePersistenceUnavailable,
		},
		"persistence failure with another current failure": {
			err: errors.Join(
				extensionsdk.Fail(controller.FailureCodeExtensionFailed, errors.New("finalized-message handler failed")),
				fmt.Errorf("append history: %w", session.ErrPersistenceUnavailable),
			),
			code: controller.FailureCodePersistenceUnavailable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Act through the accepted-run failure mapper.
			code := failureCode(test.err)

			// Assert the category of the current causes.
			require.Equal(t, test.code, code)
		})
	}
}
