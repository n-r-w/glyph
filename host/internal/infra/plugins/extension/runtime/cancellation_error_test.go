//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestReceivedErrorsSurviveLaterCancellation establishes received-error-before-cancellation ordering explicitly.
func TestReceivedErrorsSurviveLaterCancellation(t *testing.T) {
	t.Parallel()
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
			received := extensionsdk.Fail("INTERNAL", cause)
			if rejected {
				received = extensionsdk.Reject("BUSY", cause)
			}
			progress := errors.New("independent progress delivery source Ω")
			cancel()
			runtime := &Runtime{}

			// Act through both production error-selection paths after caller cancellation.
			execution := runtime.executionFailure(ctx, "tool", received, progress)
			handler := runtime.handlerOperationError(ctx, "handler", received)

			// Assert both received sources survive, with cancellation and independent progress preserved.
			require.ErrorIs(t, execution, cause)
			require.ErrorIs(t, execution, progress)
			require.ErrorIs(t, execution, context.Canceled)
			require.ErrorIs(t, handler, cause)
			require.ErrorIs(t, handler, context.Canceled)
		})
	}
}

// TestTransportCancellationRetainsDiagnostic preserves the public transport message and cancellation identity.
func TestTransportCancellationRetainsDiagnostic(t *testing.T) {
	t.Parallel()
	// Arrange an already received cancellation status with its full peer diagnostic.
	received := status.Error(codes.Canceled, "extension transport cancellation diagnostic Ω")
	runtime := &Runtime{}
	// Act through the transport mapper without caller cancellation.
	err := runtime.executionError(t.Context(), "tool", received)
	// Assert mapping keeps both the original status and cancellation identity.
	require.ErrorIs(t, err, received)
	require.ErrorContains(t, err, received.Error())
	require.Equal(t, codes.Canceled, status.Code(err))
	require.ErrorIs(t, err, context.Canceled)
}
