//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestHandlerReceivedErrorsSurviveLaterCancellation checks handler terminal causes after owner cancellation.
func TestHandlerReceivedErrorsSurviveLaterCancellation(t *testing.T) {
	t.Parallel()
	const internalFailureCode = "INTERNAL"
	for _, rejected := range []bool{false, true} {
		name := "failed"
		if rejected {
			name = "rejected"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange the SDK terminal source before canceling the caller.
			ctx, cancel := context.WithCancel(t.Context())
			cause := errors.New("received extension terminal source Ω")
			received := extensionsdk.Fail(internalFailureCode, cause)
			if rejected {
				received = extensionsdk.Reject("BUSY", cause)
			}
			cancel()
			runtime := &Runtime{}
			// Act through the production handler error-selection path after caller cancellation.
			handler := runtime.handlerOperationError(ctx, "handler", received)
			// Assert that Host retains the received handler source and owner cancellation.
			require.ErrorIs(t, handler, cause)
			require.ErrorIs(t, handler, context.Canceled)
		})
	}
}
