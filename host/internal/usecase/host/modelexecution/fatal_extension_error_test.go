//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestFatalExtensionErrorKeepsCauseAndWinsCancellation checks that a fatal extension error exposes the complete
// cause and its code, and that cancellation inside the cause does not turn it into a canceled outcome.
func TestFatalExtensionErrorKeepsCauseAndWinsCancellation(t *testing.T) {
	t.Parallel()
	// Arrange a fatal extension error whose cause is only a cancellation.
	source := NewFatalExtensionError(context.Canceled)

	// Act through the shared current-cause classification used by enclosing operation adapters.
	canceled := errtree.IsCurrentCancellation(source, func(cause error) bool {
		return errors.Is(cause, context.Canceled)
	})

	// Assert the fatal meaning, the code, and the retained cause.
	require.False(t, canceled)
	require.ErrorIs(t, source, context.Canceled)
	require.EqualError(t, source, context.Canceled.Error())
	_, fatal := errors.AsType[run.FatalOperationFailure](source)
	require.True(t, fatal)
	coded, isCoded := errors.AsType[errtree.CodedError](source)
	require.True(t, isCoded)
	require.Equal(t, string(FailureExtensionFailed), coded.Code())
}
