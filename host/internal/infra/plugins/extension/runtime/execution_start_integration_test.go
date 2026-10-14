//go:build integration

package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
)

// TestRuntimeExecutionStartRetainsReceivedStatus checks the production Start failure mapping.
func TestRuntimeExecutionStartRetainsReceivedStatus(t *testing.T) {
	t.Parallel()
	for _, code := range []codes.Code{codes.Canceled, codes.FailedPrecondition, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			t.Parallel()
			// Arrange a real registered connection that fails before a later tool is admitted.
			runtime := startHelperRuntime(t, "execute-rejection")
			_, registerErr := runtime.Register(t.Context())
			require.NoError(t, registerErr)
			received := status.Error(code, "received start transport diagnostic Ω")
			rejection := runtime.connection.Fail(received)
			callbackCalled := false
			// Act through the production Execute admission route with a live caller.
			_, cause := runtime.Execute(t.Context(), fixtureToolName, []byte("{}"), func(tool.Progress) error {
				callbackCalled = true
				return nil
			}, runtimeTestContext())
			// Assert that Host retains SDK's protocol rejection and original cause without invoking progress.
			require.NoError(t, t.Context().Err())
			require.ErrorIs(t, cause, received)
			require.ErrorContains(t, cause, received.Error())
			require.ErrorIs(t, cause, extensionruntime.ErrExtensionUnavailable)
			require.Equal(t, status.Code(rejection), status.Code(cause))
			require.False(t, callbackCalled)
			require.Equal(t, codes.FailedPrecondition, status.Code(cause))
			require.ErrorContains(t, cause, "extension protocol violation")
		})
	}
}
