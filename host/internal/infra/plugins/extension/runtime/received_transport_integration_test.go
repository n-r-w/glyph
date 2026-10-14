//go:build integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/internal/errtree"
)

const (
	// fixtureInternalFailureCode identifies an independent peer or callback failure.
	fixtureInternalFailureCode = "INTERNAL"
	// receivedTransportCancellationMode identifies the peer's admitted stream cancellation.
	receivedTransportCancellationMode = "received-transport-canceled"
	// receivedTransportCancellationAfterProgressMode identifies stream cancellation during resumed original waiting.
	receivedTransportCancellationAfterProgressMode = "received-transport-canceled-after-progress"
	// receivedTransportCancellationText contains the peer's complete cancellation diagnostic.
	receivedTransportCancellationText = "received transport cancellation diagnostic Ω"
)

// TestRuntimeReceivedTransportCancellationIdentity checks genuine received cancellation with a live caller.
func TestRuntimeReceivedTransportCancellationIdentity(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{receivedTransportCancellationMode, receivedTransportCancellationAfterProgressMode} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			// Arrange a real admitted stream and an optional independent callback interruption.
			runtime := startHelperRuntime(t, mode)
			_, registerErr := runtime.Register(t.Context())
			require.NoError(t, registerErr)
			callbackCause := errors.New("independent progress delivery source Ω")
			progress := func(tool.Progress) error {
				if mode == receivedTransportCancellationAfterProgressMode {
					return callbackCause
				}
				return nil
			}
			// Act through the production tool runtime while the caller remains active.
			_, cause := runtime.Execute(t.Context(), fixtureToolName, []byte("{}"), progress, runtimeTestContext())
			// Assert that Host retains the received status, text and unavailable cause with cancellation identity.
			require.NoError(t, t.Context().Err())
			require.ErrorContains(t, cause, receivedTransportCancellationText)
			require.ErrorIs(t, cause, extensionruntime.ErrExtensionUnavailable)
			require.Equal(t, codes.Canceled, status.Code(cause))
			require.ErrorIs(t, cause, context.Canceled)
			require.False(
				t,
				errtree.IsCurrentCancellation(
					cause,
					func(leaf error) bool { return errors.Is(leaf, context.Canceled) },
				),
			)
			if mode == receivedTransportCancellationAfterProgressMode {
				require.ErrorIs(t, cause, callbackCause)
			}
		})
	}
}
