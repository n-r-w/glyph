//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
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
