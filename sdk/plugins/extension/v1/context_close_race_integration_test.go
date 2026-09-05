//go:build integration

package extensionv1

import (
	"context"
	"io"
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

// TestCloseBeforeAutomaticCancellationRunsStaysClean verifies close precedes cancellation of the pending
// cancellation operation.
func TestCloseBeforeAutomaticCancellationRunsStaysClean(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange: connect both SDK peers through generated stream mocks with a controlled cancellation-acceptance gate.
		controller := gomock.NewController(t)
		clientService := NewMockExtensionServiceClient(controller)
		clientStream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](
			controller,
		)
		serverStream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](
			controller,
		)
		service := NewMockService(controller)
		registration := NewMockRegisterOperation(controller)
		execution := NewMockExecuteOperation(controller)
		host := NewMockHostService(controller)
		read := NewMockHostOperation(controller)
		requests := make(chan *extensionpb.OpenRequest)
		responses := make(chan *extensionpb.OpenResponse)
		cancelAcceptance := make(chan struct{})
		acceptanceGate := make(chan struct{})
		readRunning := make(chan struct{})
		releaseStarted := make(chan struct{})
		releaseGate := make(chan struct{})
		serverDone := make(chan struct{})
		var serverErr error
		var streamContext context.Context
		clientService.EXPECT().
			Open(gomock.Any()).
			Do(func(ctx context.Context, _ ...grpc.CallOption) { streamContext = ctx }).
			Return(clientStream, nil)
		serverStream.EXPECT().Context().DoAndReturn(func() context.Context { return streamContext })
		clientStream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *extensionpb.OpenRequest) error {
			if request.GetOperationId() == "extension-host-2" && request.GetEvent().GetAccepted() != nil {
				close(cancelAcceptance)
				<-acceptanceGate
			}
			select {
			case requests <- request:
				return nil
			case <-streamContext.Done():
				return streamContext.Err()
			}
		}).AnyTimes()
		clientStream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			select {
			case response, open := <-responses:
				if open {
					return response, nil
				}
				if serverErr != nil {
					return nil, status.Error(codes.Unknown, serverErr.Error())
				}
				return nil, io.EOF
			case <-streamContext.Done():
				return nil, streamContext.Err()
			}
		}).AnyTimes()
		clientStream.EXPECT().CloseSend().DoAndReturn(func() error { close(requests); return nil })
		serverStream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			select {
			case request, open := <-requests:
				if !open {
					return nil, io.EOF
				}
				return request, nil
			case <-streamContext.Done():
				return nil, streamContext.Err()
			}
		}).AnyTimes()
		serverStream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *extensionpb.OpenResponse) error {
			select {
			case responses <- response:
				return nil
			case <-streamContext.Done():
				return streamContext.Err()
			}
		}).AnyTimes()
		service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
		registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
		registration.EXPECT().Release()
		service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
		execution.EXPECT().Release()
		execution.EXPECT().
			Run(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
				binding, err := ContextFrom(ctx)
				if err != nil {
					return nil, err
				}
				startContext, cancel := context.WithCancel(ctx)
				defer cancel()
				started, err := binding.StartGetModels(startContext)
				if err != nil {
					return nil, err
				}
				<-readRunning
				cancel()
				_, err = started.Wait(ctx)
				return nil, err
			})
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(read, nil)
		read.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			close(readRunning)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		read.EXPECT().Release().Do(func() { close(releaseStarted); <-releaseGate })
		client := &Client{
			process:   nil,
			service:   clientService,
			done:      nil,
			version:   ProtocolVersion,
			closeOnce: sync.Once{},
		}
		connection, err := client.Open(t.Context())
		require.NoError(t, err)
		connection.BindHostService(host)
		go func() {
			serverErr = newServer(service).Open(serverStream)
			close(responses)
			close(serverDone)
		}()
		register := new(extensionpb.HostRequest)
		register.SetRegister(new(extensionpb.RegisterRequest))
		registered, err := connection.Start(t.Context(), "register", register)
		require.NoError(t, err)
		_, err = registered.Wait(t.Context(), nil)
		require.NoError(t, err)
		request := new(extensionpb.HostRequest)
		request.SetExecute(
			extensionpb.ExecuteRequest_builder{
				ToolName:      new("contract"),
				ArgumentsJson: []byte(`{}`),
				Context:       testInvocationIdentity(),
			}.Build(),
		)
		_, err = connection.Start(t.Context(), "execute", request)
		require.NoError(t, err)
		<-cancelAcceptance
		closed := make(chan error, 1)

		// Act: cancel Host-owned work while the automatic cancellation request has not started execution.
		go func() { closed <- connection.Close() }()
		<-releaseStarted
		close(acceptanceGate)
		synctest.Wait()

		// Assert: canceling the cancellation request during close cannot fail the peer connection.
		var prematureFailure error
		select {
		case <-serverDone:
			prematureFailure = serverErr
		default:
		}
		close(releaseGate)
		synctest.Wait()
		closeErr := <-closed
		<-serverDone
		require.NoError(t, prematureFailure, "normal close failed before the active read released")
		require.NoError(t, closeErr)
		require.NoError(t, serverErr)
	})
}
