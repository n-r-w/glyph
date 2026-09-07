//go:build integration

package uiv1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestCanceledSendUsesActualConfirmationAtCollection checks late success and still-blocked delivery for both owners.
func TestCanceledSendUsesActualConfirmationAtCollection(t *testing.T) {
	t.Parallel()
	for _, rejected := range []bool{false, true} {
		for _, completion := range []string{"blocked", "success", "failure"} {
			confirmed := completion != "blocked"
			t.Run(fmt.Sprintf("rejected=%t/completion=%s", rejected, completion), func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					// Arrange an actual Send held beyond cancellation and service cleanup held before collection.
					controller := gomock.NewController(t)
					service := NewMockService(controller)
					source := errors.New("declared source awaiting actual transport confirmation")
					transportErr := errors.New("stream canceled during actual Send")
					actualErr := errors.New("actual Send failed after cancellation")
					if rejected {
						service.EXPECT().
							PrepareInitialize(gomock.Any(), gomock.Any()).
							Return(nil, Reject("INVALID_ARGUMENT", source))
					} else {
						prepared := NewMockInitializeOperation(controller)
						service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(prepared, nil)
						prepared.EXPECT().Run(gomock.Any()).Return(nil, source)
						prepared.EXPECT().Release()
					}
					cleanupStarted, releaseCleanup := make(chan struct{}), make(chan struct{})
					service.EXPECT().Close().DoAndReturn(func() error {
						close(cleanupStarted)
						<-releaseCleanup
						return nil
					})
					ctx, cancel := context.WithCancelCause(t.Context())
					defer cancel(context.Canceled)
					stream := NewMockUIService_OpenServer[uipb.OpenRequest, uipb.OpenResponse](controller)
					stream.EXPECT().Context().Return(ctx)
					payload := new(uipb.HostRequest)
					payload.SetInitialize(new(uipb.Initialization))
					request := new(uipb.OpenRequest)
					request.SetOperationId("initialize")
					request.SetRequest(payload)
					receiveStop := make(chan struct{})
					first := stream.EXPECT().Recv().Return(request, nil)
					stream.EXPECT().Recv().DoAndReturn(func() (*uipb.OpenRequest, error) {
						<-receiveStop
						return nil, context.Canceled
					}).After(first).MaxTimes(1)
					sendStarted, releaseSend := make(chan struct{}), make(chan struct{})
					stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *uipb.OpenResponse) error {
						if response.GetEvent().GetFailed() != nil || response.GetEvent().GetRejected() != nil {
							close(sendStarted)
							<-releaseSend
							if completion == "failure" {
								return actualErr
							}
						}
						return nil
					}).AnyTimes()
					result := make(chan error, 1)

					// Act after cancellation has returned from the send wrapper and joined writer cleanup.
					go func() { result <- newServer(service).Open(stream) }()
					<-sendStarted
					cancel(transportErr)
					<-cleanupStarted
					if confirmed {
						close(releaseSend)
						synctest.Wait()
					}
					close(releaseCleanup)
					err := <-result
					if !confirmed {
						close(releaseSend)
					}
					close(receiveStop)
					synctest.Wait()

					// Assert completion uses confirmation available at collection without waiting for blocked Send.
					require.ErrorIs(t, err, transportErr)
					require.Equal(t, codes.Unavailable, status.Code(err))
					if completion == "success" {
						require.NotErrorIs(t, err, source)
						require.NotContains(t, err.Error(), source.Error())
					} else {
						require.ErrorIs(t, err, source)
					}
					if completion == "failure" {
						require.ErrorIs(t, err, actualErr)
					}
				})
			})
		}
	}
}
