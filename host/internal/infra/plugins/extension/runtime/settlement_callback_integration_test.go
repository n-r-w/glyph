//go:build integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/internal/errtree"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestRuntimeCallbackProvenance checks that the runtime waits for the original operation to settle
// after callback errors that resemble operation or connection failures.
func TestRuntimeCallbackProvenance(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{
		extensionsdk.Fail(fixtureInternalFailureCode, context.Canceled),
		status.Error(codes.Unavailable, "callback transport-shaped cause"),
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			// Arrange a valid progress event and an original terminal withheld until cancellation.
			controller := gomock.NewController(t)
			host := NewMockHostService(controller)
			nested := NewMockHostOperation(controller)
			host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(nested, nil)
			nested.EXPECT().Run(gomock.Any(), gomock.Any()).Return(nil, context.Canceled)
			nested.EXPECT().Release()
			runtime := startHelperRuntime(t, settlementPeerPrefix+"canceled-cancel-first")
			runtime.connection.BindHostService(host)
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)
			// Act through a callback that returns an error before the SDK consumes the original terminal event.
			_, result := runtime.Execute(
				t.Context(),
				fixtureToolName,
				[]byte("{}"),
				func(tool.Progress) error { return cause },
				runtimeTestContext(),
			)
			// Assert that the runtime waits for the original operation to settle and preserves the independent local cause.
			require.ErrorIs(t, result, cause)
			require.ErrorContains(t, result, "original remote cancellation diagnostic")
			require.ErrorContains(t, result, "original operation already settled")
			require.NotErrorIs(t, result, extensionruntime.ErrExtensionUnavailable)
			require.False(
				t,
				errtree.IsCurrentCancellation(
					result,
					func(leaf error) bool { return errors.Is(leaf, context.Canceled) },
				),
			)
			_, nextErr := runtime.Execute(
				t.Context(),
				fixtureToolName,
				[]byte("{}"),
				discardProgress,
				runtimeTestContext(),
			)
			require.NoError(t, nextErr)
		})
	}
}
