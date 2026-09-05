//go:build integration

package extensionv1

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestSendFailureSettlesBothInitiatorNamespaces verifies writer failure cancels owned reads and initiated waits
// together.
func TestSendFailureSettlesBothInitiatorNamespaces(t *testing.T) {
	t.Parallel()

	// Arrange: admit one extension-initiated read before failing the shared writer on an outbound invocation.
	controller := gomock.NewController(t)
	service := NewMockExtensionServiceClient(controller)
	stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	host := NewMockHostService(controller)
	read := NewMockHostOperation(controller)
	allowRequest := make(chan struct{})
	receiveGate := make(chan struct{})
	releaseReceive := sync.OnceFunc(func() { close(receiveGate) })
	running := make(chan struct{})
	released := make(chan struct{})
	service.EXPECT().Open(gomock.Any()).Return(stream, nil)
	gomock.InOrder(
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			<-allowRequest
			request := new(extensionpb.ExtensionRequest)
			request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: nil}.Build())
			return extensionpb.OpenResponse_builder{
				OperationId: new("remote-read"),
				Request:     request,
				Event:       nil,
			}.Build(), nil
		}),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
			<-receiveGate
			return nil, status.Error(codes.Canceled, "receive stopped after outbound failure")
		}),
	)
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *extensionpb.OpenRequest) error {
		if request.GetEvent() != nil {
			return nil
		}
		return status.Error(codes.Unavailable, "complete controlled outbound send failure")
	}).AnyTimes()
	stream.EXPECT().CloseSend().Return(nil)
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(read, nil)
	read.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
		close(running)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	read.EXPECT().Release().Do(func() { close(released) })
	client := &Client{process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{}}
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { assert.ErrorContains(t, connection.Close(), "complete controlled outbound send failure") })
	t.Cleanup(releaseReceive)
	connection.BindHostService(host)
	close(allowRequest)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	awaitHostDisconnectSignal(t, ctx, running, "accepted Host read")
	request := new(extensionpb.HostRequest)
	request.SetExecute(
		extensionpb.ExecuteRequest_builder{
			ToolName:      new("tool"),
			ArgumentsJson: []byte(`{}`),
			Context:       testInvocationIdentity(),
		}.Build(),
	)

	// Act: fail an outbound send while both operation namespaces have owned state.
	started, err := connection.Start(ctx, "host-invocation", request)
	require.NoError(t, err)
	_, err = started.Wait(ctx, nil)
	releaseReceive()

	// Assert: failure text is complete, owned Host work releases, and connection close joins both directions.
	require.ErrorContains(t, err, "complete controlled outbound send failure")
	assert.Equal(t, codes.Unavailable, status.Code(err))
	awaitHostDisconnectSignal(t, ctx, released, "owned Host read release")
	require.ErrorContains(t, connection.Close(), "complete controlled outbound send failure")
}
