//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
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
