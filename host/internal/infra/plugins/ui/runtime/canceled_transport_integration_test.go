//go:build integration

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	"github.com/n-r-w/glyph/internal/operation"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestHostUICanceledTransport checks cancellation diagnostics through live UI Wait and notification readers.
func TestHostUICanceledTransport(t *testing.T) {
	t.Parallel()
	const independentFailureCode = "PROVIDER_UNAVAILABLE"
	// Arrange the real UI stream and SDK Host, with a mock for the external UI application.
	controller := gomock.NewController(t)
	service := NewMockSDKUIService(controller)
	initialization := NewMockSDKInitializeOperation(controller)
	hosts := make(chan *uisdk.Host, 1)
	serviceClosed := make(chan struct{})
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initialization, nil)
	initialization.EXPECT().Run(gomock.Any()).Return(new(uipb.Initialized), nil)
	initialization.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
			hosts <- host
			<-ctx.Done()
			return context.Cause(ctx)
		})
	service.EXPECT().Close().DoAndReturn(func() error { close(serviceClosed); return nil })
	client := uisdk.TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	initializeRequest := new(uipb.HostRequest)
	initializeRequest.SetInitialize(new(uipb.Initialization))
	require.NoError(t, stream.Send(uipb.OpenRequest_builder{
		OperationId: new("initialize"), Request: initializeRequest, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	host := <-hosts
	submit := new(uipb.UIRequest)
	submit.SetSubmit(
		uipb.SubmitCommand_builder{
			Content: []*uipb.UserContent{uipb.UserContent_builder{Text: new("conversation"), Image: nil}.Build()},
		}.Build(),
	)
	original, err := host.Start(t.Context(), "host-ui-canceled", submit)
	require.NoError(t, err)
	request, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "host-ui-canceled", request.GetOperationId())
	localContext, cancelLocal := context.WithCancel(t.Context())
	cancelLocal()
	_, localErr := original.Wait(localContext)
	require.ErrorIs(t, localErr, context.Canceled)
	assert.True(t, errtree.IsPlainCancellation(localErr, func(err error) bool {
		return errors.Is(err, context.Canceled)
	}))

	ctx, cancel := context.WithCancel(t.Context())
	messages := make(chan *uipb.OpenRequest, 1)
	writer, constructionErr := operation.NewWriter(func(message *uipb.OpenRequest) error {
		encoded, err := proto.Marshal(message)
		if err != nil {
			return err
		}
		received := new(uipb.OpenRequest)
		if err := proto.Unmarshal(encoded, received); err != nil {
			return err
		}
		messages <- received
		return stream.Send(message)
	})
	require.NoError(t, constructionErr)
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = writer.Run(ctx) }()
	defer func() { writer.Close(); cancel(); <-stopped }()
	failures := make(chan error, 1)
	delivery := &operationDelivery{
		ctx:    ctx,
		writer: writer,
		fail:   func(cause error) { failures <- cause },
		mutex:  sync.Mutex{},
		kinds:  nil,
	}
	source := diagnostic.NewCurrentCause(
		context.Canceled,
		errors.New("Unicode: λ\n"+strings.Repeat("full diagnostic ", 512)+"  "),
	)
	// Act through the Host lifecycle encoder and the SDK's tracked original operation.
	accepted, err := delivery.Accepted("host-ui-canceled")
	require.NoError(t, err)
	require.NoError(t, accepted.Wait(t.Context()))
	<-messages
	require.NoError(t, delivery.Running("host-ui-canceled"))
	<-messages
	acknowledgement, err := delivery.Terminal(
		"host-ui-canceled",
		operation.CanceledWithSource[controllerui.OperationResult](source),
	)
	require.NoError(t, err)
	require.NoError(t, acknowledgement.Wait(t.Context()))
	received := <-messages
	// Assert diagnostics through both live SDK readers before checking the serialized sender frame.
	_, waitErr := original.Wait(t.Context())
	notification, err := host.Receive(t.Context())
	require.NoError(t, err)
	require.Equal(t, "host-ui-canceled", notification.OperationID())
	for _, cause := range []error{waitErr, notification.OperationError()} {
		var canceled *uisdk.CanceledError
		require.ErrorAs(t, cause, &canceled)
		require.ErrorIs(t, cause, context.Canceled)
		assert.Contains(t, cause.Error(), source.Error())
		assert.True(t, errtree.IsCurrentCancellation(cause, func(err error) bool {
			return errors.Is(err, context.Canceled)
		}))
		assert.False(t, errtree.IsPlainCancellation(cause, func(err error) bool {
			return errors.Is(err, context.Canceled)
		}))
		wrapped := fmt.Errorf("UI receiver: %w", cause)
		var wrappedCanceled *uisdk.CanceledError
		require.ErrorAs(t, wrapped, &wrappedCanceled)
		assert.Same(t, canceled, wrappedCanceled)
		require.ErrorIs(t, wrapped, cause)
	}

	require.True(t, received.GetEvent().HasCanceled())
	require.Equal(t, source.Error(), received.GetEvent().GetCanceled().GetMessage())
	require.Empty(t, failures)

	// Act through the same tracked connection for plain cancellation and an independent coded failure.
	for _, scenario := range []struct {
		id      string
		outcome operation.Outcome[controllerui.OperationResult]
		plain   bool
	}{
		{id: "plain", outcome: operation.Canceled[controllerui.OperationResult](), plain: true},
		{id: "failed", outcome: operation.Failed[controllerui.OperationResult](
			independentFailureCode, errors.New("independent provider failure")), plain: false},
	} {
		started, startErr := host.Start(t.Context(), scenario.id, submit)
		require.NoError(t, startErr)
		request, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		require.Equal(t, scenario.id, request.GetOperationId())
		accepted, admissionErr := delivery.Accepted(scenario.id)
		require.NoError(t, admissionErr)
		require.NoError(t, accepted.Wait(t.Context()))
		<-messages
		require.NoError(t, delivery.Running(scenario.id))
		<-messages
		acknowledgement, terminalErr := delivery.Terminal(scenario.id, scenario.outcome)
		require.NoError(t, terminalErr)
		require.NoError(t, acknowledgement.Wait(t.Context()))
		<-messages
		_, terminalErr = started.Wait(t.Context())
		notification, receiveErr := host.Receive(t.Context())
		require.NoError(t, receiveErr)
		require.Equal(t, scenario.id, notification.OperationID())
		// Assert that each live reader keeps cancellation distinct from independent failure.
		for _, cause := range []error{terminalErr, notification.OperationError()} {
			if scenario.plain {
				require.ErrorIs(t, cause, context.Canceled)
				assert.True(t, errtree.IsPlainCancellation(cause, func(err error) bool {
					return errors.Is(err, context.Canceled)
				}))
				continue
			}
			var failed *uisdk.FailureError
			require.ErrorAs(t, cause, &failed)
			assert.Equal(t, independentFailureCode, failed.Code())
			require.ErrorContains(t, cause, "independent provider failure")
			assert.False(t, errtree.IsCurrentCancellation(cause, func(err error) bool {
				return errors.Is(err, context.Canceled)
			}))
		}
	}
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	<-serviceClosed
}
