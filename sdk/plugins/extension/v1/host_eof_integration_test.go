//go:build integration

package extensionv1

import (
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestConnectionSendEOFWaitsForReceive verifies pending invocation settlement and joined cleanup.
func TestConnectionSendEOFWaitsForReceive(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a pending invocation with send EOF before the definitive receive status.
		controller := gomock.NewController(t)
		service := NewMockExtensionServiceClient(controller)
		stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
		service.EXPECT().Open(gomock.Any()).Return(stream, nil)
		receiveEntered := make(chan struct{})
		resolveReceive := make(chan struct{})
		sendReturned := make(chan struct{})
		remoteText := "remote Extension failure: Ω\ncomplete second line"
		remoteErr := status.Error(codes.DataLoss, remoteText)
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			close(receiveEntered)
			<-resolveReceive
			return nil, remoteErr
		})
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*extensionpb.OpenRequest) error {
			<-receiveEntered
			close(sendReturned)
			return io.EOF
		})
		cleanupEntered := make(chan struct{})
		resolveCleanup := make(chan struct{})
		stream.EXPECT().CloseSend().DoAndReturn(func() error {
			close(cleanupEntered)
			<-resolveCleanup
			return nil
		})
		client := &Client{
			process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{},
		}
		connection, err := client.Open(t.Context())
		require.NoError(t, err)
		request := new(extensionpb.HostRequest)
		request.SetExecute(new(extensionpb.ExecuteRequest))
		started, err := connection.Start(t.Context(), "pending", request)
		require.NoError(t, err)
		waitResult := make(chan error, 1)
		go func() {
			_, waitErr := started.Wait(t.Context(), nil)
			waitResult <- waitErr
		}()

		// Act with the receiver held after the send-side EOF.
		<-sendReturned
		synctest.Wait()
		select {
		case waitErr := <-waitResult:
			assert.Fail(t, "invocation settled before receive resolution", "error: %v", waitErr)
			waitResult <- waitErr
		default:
		}
		close(resolveReceive)
		waitErr := <-waitResult
		synctest.Wait()
		delivery := &hostDelivery{connection: connection}
		runningErr := delivery.Running("after-stop")
		_, acceptedErr := delivery.Accepted("after-stop")
		closeResult := make(chan error, 1)
		go func() { closeResult <- connection.Close() }()
		<-cleanupEntered
		synctest.Wait()
		select {
		case closeErr := <-closeResult:
			assert.Fail(t, "cleanup returned before CloseSend joined", "error: %v", closeErr)
			closeResult <- closeErr
		default:
		}
		close(resolveCleanup)
		closeErr := <-closeResult

		// Assert invocation, stopped-writer producers, subsequent initiation and cleanup retain the status.
		assert.Equal(t, codes.DataLoss, status.Code(waitErr))
		require.ErrorContains(t, waitErr, remoteText)
		require.ErrorIs(t, waitErr, remoteErr)
		for _, deliveryErr := range []error{runningErr, acceptedErr} {
			assert.Equal(t, codes.DataLoss, status.Code(deliveryErr))
			require.ErrorContains(t, deliveryErr, remoteText)
			require.ErrorIs(t, deliveryErr, remoteErr)
		}
		_, enqueueErr := connection.Start(t.Context(), "after-stop", request)
		assert.Equal(t, codes.DataLoss, status.Code(enqueueErr))
		require.ErrorContains(t, enqueueErr, remoteText)
		assert.Equal(t, codes.DataLoss, status.Code(closeErr))
		assert.ErrorContains(t, closeErr, remoteText)
	})
}

// TestConnectionCleanReceiveEOFResolvesOwnedAcknowledgements verifies clean closure without a join cycle.
func TestConnectionCleanReceiveEOFResolvesOwnedAcknowledgements(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange an original invocation and nested Host work waiting for its acceptance send.
		controller := gomock.NewController(t)
		service := NewMockExtensionServiceClient(controller)
		stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
		service.EXPECT().Open(gomock.Any()).Return(stream, nil)
		host := NewMockHostService(controller)
		prepared := NewMockHostOperation(controller)
		released := make(chan struct{})
		prepared.EXPECT().Release().Do(func() { close(released) })
		host.EXPECT().Prepare(gomock.Any(), "nested", gomock.Any()).Return(prepared, nil)
		startReceive := make(chan struct{})
		resolveReceive := make(chan struct{})
		nested := new(extensionpb.ExtensionRequest)
		nested.SetGetModels(new(extensionpb.GetModelsRequest))
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			<-startReceive
			return extensionpb.OpenResponse_builder{
				OperationId: new("nested"), Event: nil, Request: nested,
			}.Build(), nil
		})
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			<-resolveReceive
			return nil, io.EOF
		})
		requestSent := make(chan struct{})
		sendReturned := make(chan struct{})
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *extensionpb.OpenRequest) error {
			assert.Equal(t, "pending", request.GetOperationId())
			close(requestSent)
			return nil
		})
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *extensionpb.OpenRequest) error {
			assert.NotNil(t, request.GetEvent().GetAccepted())
			close(sendReturned)
			return io.EOF
		})
		stream.EXPECT().CloseSend().Return(nil)
		client := &Client{
			process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{},
		}
		connection, err := client.Open(t.Context())
		require.NoError(t, err)
		connection.BindHostService(host)
		request := new(extensionpb.HostRequest)
		request.SetExecute(new(extensionpb.ExecuteRequest))
		started, err := connection.Start(t.Context(), "pending", request)
		require.NoError(t, err)
		<-requestSent
		close(startReceive)
		waitResult := make(chan error, 1)
		go func() {
			_, waitErr := started.Wait(t.Context(), nil)
			waitResult <- waitErr
		}()

		// Act with nested acceptance returning send EOF before clean receive closure.
		<-sendReturned
		synctest.Wait()
		select {
		case waitErr := <-waitResult:
			assert.Fail(t, "invocation settled before clean receive EOF", "error: %v", waitErr)
			waitResult <- waitErr
		default:
		}
		close(resolveReceive)
		waitErr := <-waitResult
		closeErr := connection.Close()

		// Assert clean receive semantics and release of acknowledgement-dependent work before return.
		assert.Equal(t, codes.FailedPrecondition, status.Code(waitErr))
		require.ErrorContains(t, waitErr, "stream closed before terminal event")
		require.NoError(t, closeErr)
		select {
		case <-released:
		default:
			t.Fatal("cleanup returned before nested Host work released")
		}
	})
}
