//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServerRejectedSourceSurvivesEOFDrain checks original rejection and independent writer cleanup causes.
func TestServerRejectedSourceSurvivesEOFDrain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a rejected registration and EOF while its output remains in Send.
		controller := gomock.NewController(t)
		service := NewMockService(controller)
		source := errors.Join(errors.New("complete registration rejection source"), operation.ErrQueueFull)
		cleanup := errors.New("independent preparation cleanup source")
		original := fmt.Errorf(
			"outer rejection source: %w",
			errors.Join(Reject(rejectionCodeNotReady, source), cleanup),
		)
		service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(nil, original)
		stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
		stream.EXPECT().Context().Return(t.Context())
		payload := new(extensionpb.HostRequest)
		payload.SetRegister(new(extensionpb.RegisterRequest))
		request := new(extensionpb.OpenRequest)
		request.SetOperationId("registration")
		request.SetRequest(payload)
		sending, release := make(chan struct{}), make(chan struct{})
		gomock.InOrder(
			stream.EXPECT().Recv().Return(request, nil),
			stream.EXPECT().
				Recv().
				DoAndReturn(func() (*extensionpb.OpenRequest, error) { <-sending; return nil, io.EOF }),
		)
		writeErr := errors.New("independent rejected-output write failure")
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *extensionpb.OpenResponse) error {
			assert.Equal(t, rejectionCodeNotReady, response.GetEvent().GetRejected().GetCode())
			close(sending)
			<-release
			return errors.Join(context.Canceled, writeErr)
		})
		result := make(chan error, 1)

		// Act through EOF cleanup before the pending rejection Send fails.
		go func() { result <- newServer(service).Open(stream) }()
		synctest.Wait()
		close(release)
		err := <-result

		// Assert source lookalikes do not reclassify delivery or replace independent causes.
		assert.Equal(t, codes.Unavailable, status.Code(err))
		require.ErrorIs(t, err, source)
		require.ErrorIs(t, err, cleanup)
		require.ErrorIs(t, err, writeErr)
		assert.ErrorContains(t, err, original.Error())
	})
}

// TestCanceledServerSendUsesActualConfirmationAtCollection verifies all nonblocking confirmation outcomes.
func TestCanceledServerSendUsesActualConfirmationAtCollection(t *testing.T) {
	t.Parallel()
	for _, completion := range []string{"blocked", "success", "failure"} {
		t.Run(completion, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange failed handler work and a terminal Send held beyond connection cancellation.
				controller := gomock.NewController(t)
				service := NewMockService(controller)
				source := errors.New("handler terminal source")
				connectionErr := errors.New("connection cancellation while Send is active")
				actualErr := errors.New("actual Send failed after cancellation")
				cleanupStarted, releaseCleanup := make(chan struct{}), make(chan struct{})
				blocker := NewMockHandleOperation(controller)
				blocker.EXPECT().
					Run(gomock.Any()).
					DoAndReturn(func(ctx context.Context) (*extensionpb.HandleResponse, error) {
						<-ctx.Done()
						return nil, context.Canceled
					}).
					MaxTimes(1)
				blocker.EXPECT().Release().Do(func() {
					close(cleanupStarted)
					<-releaseCleanup
				})
				prepared := NewMockHandleOperation(controller)
				prepared.EXPECT().Run(gomock.Any()).Return(nil, source)
				prepared.EXPECT().Release()
				gomock.InOrder(
					service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).Return(blocker, nil),
					service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).Return(prepared, nil),
				)
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(context.Canceled)
				stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](
					controller,
				)
				stream.EXPECT().Context().Return(ctx)
				server := newServer(service)
				server.completeRegistration(extensionpb.RegisterResponse_builder{
					Tools: nil,
					Handlers: []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
						Id: new("handler"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
					}.Build()},
				}.Build())
				newHandleRequest := func(id string) *extensionpb.OpenRequest {
					payload := new(extensionpb.HostRequest)
					payload.SetHandle(extensionpb.HandleRequest_builder{
						ModelSelection: nil, ReasoningSelection: nil,
						Context: testInvocationIdentity(), HandlerId: new("handler"),
						SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, Lifecycle: nil,
						SessionTree: extensionpb.SessionTreeInvocation_builder{
							SessionId: new("session"), TargetEntryId: new("target"), PrecedingActiveLeafId: nil,
							NavigationDestinationId: nil, CommittedActiveLeafId: nil, CreatedSummary: nil,
						}.Build(),
					}.Build())
					request := new(extensionpb.OpenRequest)
					request.SetOperationId(id)
					request.SetRequest(payload)
					return request
				}
				receiveStop := make(chan struct{})
				gomock.InOrder(
					stream.EXPECT().Recv().Return(newHandleRequest("cleanup-blocker"), nil),
					stream.EXPECT().Recv().Return(newHandleRequest("failed-handler"), nil),
					stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
						<-receiveStop
						return nil, context.Canceled
					}),
				)
				sendStarted, releaseSend := make(chan struct{}), make(chan struct{})
				sendReturned := make(chan struct{})
				stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *extensionpb.OpenResponse) error {
					if response.GetEvent().GetFailed() != nil {
						close(sendStarted)
						<-releaseSend
						close(sendReturned)
						if completion == "failure" {
							return actualErr
						}
					}
					return nil
				}).AnyTimes()
				result := make(chan error, 1)

				// Act after cancellation has released the writer and cleanup is held before collection.
				go func() { result <- server.Open(stream) }()
				<-sendStarted
				cancel(connectionErr)
				<-cleanupStarted
				if completion != "blocked" {
					close(releaseSend)
					<-sendReturned
					synctest.Wait()
				}
				close(releaseCleanup)
				err := <-result
				if completion == "blocked" {
					select {
					case <-sendReturned:
						require.Fail(t, "raw send returned before handler cleanup")
					default:
					}
					close(releaseSend)
					<-sendReturned
				}
				close(receiveStop)
				synctest.Wait()

				// Assert collection uses only the actual confirmation available at its boundary.
				require.ErrorIs(t, err, connectionErr)
				assert.Equal(t, codes.Unavailable, status.Code(err))
				if completion == "success" {
					require.NotErrorIs(t, err, source)
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

// TestHostRejectedSourceSurvivesConnectionClose checks Host preparation causes through connection cleanup.
func TestHostRejectedSourceSurvivesConnectionClose(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a classified Host rejection with an outer wrapper and independent preparation cause.
		controller := gomock.NewController(t)
		service := NewMockExtensionServiceClient(controller)
		stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
		host := NewMockHostService(controller)
		source := errors.New("complete Host rejection source")
		cleanup := errors.New("independent Host preparation cleanup")
		original := fmt.Errorf("outer Host rejection: %w", errors.Join(Reject(rejectionCodeNotReady, source), cleanup))
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, original)
		service.EXPECT().Open(gomock.Any()).Return(stream, nil)
		admit, stopped := make(chan struct{}), make(chan struct{})
		request := new(extensionpb.ExtensionRequest)
		request.SetGetModels(new(extensionpb.GetModelsRequest))
		gomock.InOrder(
			stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
				<-admit
				return extensionpb.OpenResponse_builder{
					OperationId: new("models"),
					Request:     request,
					Event:       nil,
				}.Build(), nil
			}),
			stream.EXPECT().
				Recv().
				DoAndReturn(func() (*extensionpb.OpenResponse, error) { <-stopped; return nil, io.EOF }),
		)
		writeErr := errors.New("Host rejection Send failed")
		stream.EXPECT().Send(gomock.Any()).Return(writeErr)
		stream.EXPECT().CloseSend().DoAndReturn(func() error { close(stopped); return nil })
		client := &Client{process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{}}
		connection, err := client.Open(t.Context())
		require.NoError(t, err)
		connection.BindHostService(host)

		// Act through rejected output failure and joined connection Close.
		close(admit)
		synctest.Wait()
		err = connection.Close()

		// Assert the local completion retains both original causes and the delivery error.
		require.ErrorIs(t, err, source)
		require.ErrorIs(t, err, cleanup)
		require.ErrorIs(t, err, writeErr)
		assert.ErrorContains(t, err, original.Error())
	})
}
