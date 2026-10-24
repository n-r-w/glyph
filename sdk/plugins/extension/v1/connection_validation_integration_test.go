//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestConnectionFailNilLeavesConnectionUsable checks work admission and delivery after invalid failure reporting.
func TestConnectionFailNilLeavesConnectionUsable(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a healthy connection over the existing generated transport mocks.
		controller := gomock.NewController(t)
		service := NewMockExtensionServiceClient(controller)
		stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
		var rpcContext context.Context
		service.EXPECT().Open(gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ ...grpc.CallOption) (extensionpb.ExtensionService_OpenClient, error) {
				rpcContext = ctx
				return stream, nil
			},
		)
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			<-rpcContext.Done()
			return nil, status.Error(codes.Canceled, "connection stopped")
		})
		stream.EXPECT().CloseSend().Return(nil)
		sent := make(chan *extensionpb.OpenRequest, 1)
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *extensionpb.OpenRequest) error {
			sent <- request
			return nil
		})
		client := &Client{process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{}}
		connection, err := client.Open(t.Context())
		require.NoError(t, err)
		defer func() { _ = connection.Close() }()
		request := new(extensionpb.HostRequest)
		request.SetRegister(new(extensionpb.RegisterRequest))

		// Act with invalid failure reporting followed by ordinary work on the same connection.
		validation := connection.Fail(nil)
		started, startErr := connection.Start(t.Context(), "after-invalid-failure", request)

		// Assert validation did not cancel the RPC or prevent actual transport delivery.
		require.ErrorContains(t, validation, "cause is required")
		require.NoError(t, context.Cause(rpcContext))
		require.NoError(t, startErr)
		require.NotNil(t, started)
		require.Equal(t, "after-invalid-failure", (<-sent).GetOperationId())

		// Act with a deliberate valid failure after the successful send.
		cause := errors.New("deliberate shutdown Ω\ncomplete second line")
		mapped := connection.Fail(cause)
		_, waitErr := started.Wait(t.Context(), nil)

		// Assert valid failure still cancels the connection and retains its complete cause.
		require.Equal(t, codes.FailedPrecondition, status.Code(mapped))
		require.ErrorContains(t, mapped, cause.Error())
		require.ErrorIs(t, mapped, cause)
		require.ErrorIs(t, context.Cause(rpcContext), cause)
		require.ErrorIs(t, waitErr, cause)
	})
}
