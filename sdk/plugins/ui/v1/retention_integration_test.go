//go:build integration

package uiv1

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestSDKRetainsRejectedInitializationSource exercises rejected output and processing joined during cancellation.
func TestSDKRetainsRejectedInitializationSource(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"direct rejection", "late rejection", "late failure"} {
		late := name != "direct rejection"
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a rejected initialization whose producer can finish during connection cleanup.
				controller := gomock.NewController(t)
				source := errors.Join(
					errors.New("complete SDK initialization admission source"), context.Canceled,
					status.Error(codes.ResourceExhausted, "source status must not classify transport"),
				)
				deliveryErr := errors.New("SDK rejection transport stopped")
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(context.Canceled)
				started := make(chan struct{})
				release := make(chan struct{})
				service := NewMockService(controller)
				service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, *uipb.Initialization) (InitializeOperation, error) {
						close(started)
						if late {
							<-release
						}
						if name == "late failure" {
							return nil, source
						}
						return nil, Reject("INVALID_ARGUMENT", source)
					},
				)
				service.EXPECT().Close().Return(nil)
				stream := NewMockUIService_OpenServer[uipb.OpenRequest, uipb.OpenResponse](controller)
				stream.EXPECT().Context().Return(ctx)
				request := new(uipb.OpenRequest)
				request.SetOperationId("initialize")
				payload := new(uipb.HostRequest)
				payload.SetInitialize(new(uipb.Initialization))
				request.SetRequest(payload)
				stopReceive := make(chan struct{})
				first := stream.EXPECT().Recv().Return(request, nil)
				stream.EXPECT().Recv().DoAndReturn(func() (*uipb.OpenRequest, error) {
					<-stopReceive
					return nil, context.Canceled
				}).After(first).MaxTimes(1)
				stream.EXPECT().Send(gomock.Any()).Return(deliveryErr).MaxTimes(1)
				result := make(chan error, 1)

				// Act without releasing raw Recv until the SDK handler has returned.
				go func() { result <- newServer(service).Open(stream) }()
				<-started
				if late {
					cancel(deliveryErr)
					synctest.Wait()
				}
				close(release)
				err := <-result
				close(stopReceive)
				synctest.Wait()

				// Assert late preparation and rejection sources cannot change the transport category.
				require.Equal(t, codes.Unavailable, status.Code(err))
				require.ErrorContains(t, err, source.Error())
				require.ErrorIs(t, err, source)
				require.ErrorIs(t, err, deliveryErr)
			})
		})
	}
}

// TestSDKRetainsInitializationFailureAtCompletion exercises every undelivered terminal lifetime.
func TestSDKRetainsInitializationFailureAtCompletion(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"before terminal", "queued terminal", "terminal send"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange failed initialization with a source category distinct from transport failure.
				controller := gomock.NewController(t)
				source := errors.Join(errors.New("complete initialization work source"), operation.ErrQueueFull)
				deliveryErr := errors.New("UI SDK transport failed")
				cleanupErr := errors.New("UI SDK cleanup failed")
				service := NewMockService(controller)
				prepared := NewMockInitializeOperation(controller)
				service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(prepared, nil)
				service.EXPECT().Close().Return(cleanupErr)
				prepared.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*uipb.Initialized, error) {
					if mode == "before terminal" {
						<-ctx.Done()
					}
					return nil, source
				})
				prepared.EXPECT().Release()
				stream := NewMockUIService_OpenServer[uipb.OpenRequest, uipb.OpenResponse](controller)
				stream.EXPECT().Context().Return(t.Context())
				request := new(uipb.OpenRequest)
				request.SetOperationId("initialize")
				payload := new(uipb.HostRequest)
				payload.SetInitialize(new(uipb.Initialization))
				request.SetRequest(payload)
				receiveStop := make(chan struct{})
				gomock.InOrder(
					stream.EXPECT().Recv().Return(request, nil),
					stream.EXPECT().Recv().DoAndReturn(func() (*uipb.OpenRequest, error) {
						<-receiveStop
						return nil, context.Canceled
					}),
				)
				releaseSend := make(chan struct{})
				stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *uipb.OpenResponse) error {
					if response.GetEvent().GetRunning() != nil && mode != "terminal send" {
						<-releaseSend
						return deliveryErr
					}
					if response.GetEvent().GetFailed() != nil {
						return deliveryErr
					}
					return nil
				}).AnyTimes()
				result := make(chan error, 1)

				// Act through the actual SDK server while raw Recv remains blocked until after handler return.
				go func() { result <- newServer(service).Open(stream) }()
				synctest.Wait()
				close(releaseSend)
				err := <-result
				close(receiveStop)
				synctest.Wait()

				// Assert final completion retains source and independent cleanup without category leakage.
				require.ErrorContains(t, err, "complete initialization work source")
				require.ErrorIs(t, err, source)
				require.ErrorIs(t, err, deliveryErr)
				require.ErrorIs(t, err, cleanupErr)
				require.Equal(t, codes.Unavailable, status.Code(err))
			})
		})
	}
}
