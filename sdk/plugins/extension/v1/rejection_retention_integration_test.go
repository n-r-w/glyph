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
