//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestConnectionFailPreservesValidationCause checks failure reporting and owner-joined transport cleanup.
func TestConnectionFailPreservesValidationCause(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange: hold resource cleanup after the RPC observes cancellation.
		controller := gomock.NewController(t)
		service := NewMockExtensionServiceClient(controller)
		stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
		var rpcContext context.Context
		service.EXPECT().
			Open(gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ ...grpc.CallOption) (extensionpb.ExtensionService_OpenClient, error) {
				rpcContext = ctx
				return stream, nil
			})
		cleanupGate := make(chan struct{})
		releaseCleanup := sync.OnceFunc(func() { close(cleanupGate) })
		cleanupCause := errors.New("transport cleanup failed")
		validationCause := errors.New("result contents are empty")
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			<-rpcContext.Done()
			return nil, status.Error(codes.Canceled, "failed connection canceled receive")
		})
		stream.EXPECT().CloseSend().DoAndReturn(func() error {
			<-cleanupGate
			return cleanupCause
		})
		client := &Client{process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{}}
		connection, err := client.Open(t.Context())
		require.NoError(t, err)
		defer func() { releaseCleanup(); _ = connection.Close() }()
		failureResult := make(chan error, 1)
		closeResult := make(chan error, 1)

		// Act: report failure while transport cleanup cannot complete.
		go func() { failureResult <- connection.Fail(validationCause) }()
		synctest.Wait()

		// Assert: reporting returns its cause and cancels the RPC before cleanup is released.
		select {
		case err = <-failureResult:
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			require.ErrorContains(t, err, validationCause.Error())
			require.ErrorIs(t, err, validationCause)
		default:
			t.Error("Fail waited for transport cleanup")
			releaseCleanup()
			<-failureResult
			require.ErrorIs(t, connection.Close(), cleanupCause)
			return
		}
		require.ErrorIs(t, context.Cause(rpcContext), validationCause)
		request := new(extensionpb.HostRequest)
		request.SetRegister(new(extensionpb.RegisterRequest))
		_, err = connection.Start(t.Context(), "after-failure", request)
		require.ErrorIs(t, err, validationCause)
		go func() { closeResult <- connection.Close() }()
		synctest.Wait()
		select {
		case <-closeResult:
			t.Error("Close returned before transport cleanup completed")
			return
		default:
		}
		releaseCleanup()
		err = <-closeResult
		require.ErrorIs(t, err, validationCause)
		require.ErrorIs(t, err, cleanupCause)
		require.ErrorContains(t, err, cleanupCause.Error())
		require.ErrorIs(t, connection.CompletionFailures(), cleanupCause)
		require.ErrorIs(t, connection.Close(), cleanupCause)
	})
}
