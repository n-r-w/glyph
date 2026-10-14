//go:build integration

package runtime

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestRunOperationsSendEOFRetainsReceiveStatus verifies EOF resolution through buffered input and cleanup.
func TestRunOperationsSendEOFRetainsReceiveStatus(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{
		"buffered response", "acquired status", "requested close", "terminal enqueue", "half-close failure",
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange send EOF while the sole receiver has not yet supplied the remote status.
				controller := gomock.NewController(t)
				stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
				transport := New()
				transport.stream = stream
				transport.ready = true
				var canceled atomic.Bool
				transport.cancel = func() { canceled.Store(true) }
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				requestedClose := mode == "requested close"
				if requestedClose {
					cancel()
				}
				sendReturned := make(chan struct{})
				resolveReceive := make(chan struct{})
				finishWork := make(chan struct{})
				released := make(chan struct{})
				localCause := errors.New("undelivered UI operation cause: λ\ncomplete local line")
				prepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](controller)
				if !requestedClose {
					prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
						workContext context.Context,
						reporter operation.Reporter[controllerui.Frame],
					) operation.Outcome[controllerui.Frame] {
						_ = reporter.Report(testLifecycleFrame())
						<-finishWork
						return operation.Failed[controllerui.Frame](
							controllerui.FailureCodeInternal, errors.Join(localCause, context.Cause(workContext)),
						)
					})
					prepared.EXPECT().Release().Do(func() { close(released) })
				}
				remoteText := "remote UI failure: Ω\ncomplete second line"
				remoteErr := status.Error(codes.DataLoss, remoteText)
				var receives atomic.Int32
				var receivedStatus atomic.Bool
				stream.EXPECT().Recv().AnyTimes().DoAndReturn(func() (*uiv1.OpenResponse, error) {
					call := receives.Add(1)
					if !requestedClose && call == 1 {
						return operationRequest("active", "work"), nil
					}
					<-resolveReceive
					bufferedCall := int32(2)
					if requestedClose {
						bufferedCall = 1
					}
					if mode != "acquired status" && call == bufferedCall {
						response := new(uiv1.OpenResponse)
						response.SetClose(new(operationv1.CloseConnection))
						return response, nil
					}
					receivedStatus.Store(true)
					return nil, remoteErr
				})
				var sends atomic.Int32
				stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(*uiv1.OpenRequest) error {
					call := sends.Add(1)
					if requestedClose || call == 3 {
						close(sendReturned)
						return io.EOF
					}
					return nil
				})
				var halfClosed atomic.Bool
				closeCause := errors.New("UI half-close cause: δ\ncomplete close line")
				stream.EXPECT().CloseSend().DoAndReturn(func() error {
					halfClosed.Store(true)
					if mode == "half-close failure" {
						return closeCause
					}
					return nil
				})
				result := make(chan error, 1)
				go func() {
					result <- runTestOperations(t, transport, ctx, func() {}, func(
						context.Context, controllerui.Command,
					) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
						return prepared, nil
					})
				}()

				// Act after Send returns EOF, while the receiver waits to deliver the definitive remote status.
				// Except in "acquired status" mode, the receiver first returns a buffered response.
				<-sendReturned
				if mode == "terminal enqueue" {
					close(finishWork)
				}
				synctest.Wait()
				assert.False(t, canceled.Load(), "send EOF canceled receive before status resolution")
				select {
				case err := <-result:
					assert.Fail(t, "session returned before receive resolution", "error: %v", err)
					result <- err
				default:
				}
				if mode != "terminal enqueue" {
					close(finishWork)
				}
				close(resolveReceive)
				err := <-result

				// Assert complete remote and local causes survive and all owned work has joined.
				require.Error(t, err)
				assert.Equal(t, codes.DataLoss, status.Code(err))
				require.ErrorContains(t, err, remoteText)
				require.ErrorIs(t, err, remoteErr)
				assert.True(t, receivedStatus.Load())
				assert.True(t, halfClosed.Load())
				if mode == "half-close failure" {
					require.ErrorIs(t, err, closeCause)
					require.ErrorContains(t, err, closeCause.Error())
				}
				if !requestedClose {
					require.ErrorIs(t, err, localCause)
					require.ErrorContains(t, err, localCause.Error())
					select {
					case <-released:
					default:
						t.Fatal("session returned before prepared work released")
					}
				}
			})
		})
	}
}
