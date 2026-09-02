//go:build integration

package uiv1

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const uiSDKHelperEnvironment = "GLYPH_UI_SDK_HELPER"

// TestSDKHelperProcess serves the UI contract when this test binary is a plugin child.
func TestSDKHelperProcess(t *testing.T) {
	t.Parallel()
	// Arrange the helper mode and generated service mock for the plugin child process.
	// Act by calling Serve or the mismatched handshake server selected by the helper mode.
	// Assert the child process exposes only the selected UI protocol behavior.

	controller := gomock.NewController(t)
	switch os.Getenv(uiSDKHelperEnvironment) {
	case "serve":
		Serve(newProcessMockService(controller))
	case "version-mismatch":
		service := NewMockService(controller)
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: handshakeConfig(), TLSProvider: nil, Plugins: nil,
			VersionedPlugins: map[int]plugin.PluginSet{99: pluginSets(newServer(service))[ProtocolVersion]},
			GRPCServer:       plugin.DefaultGRPCServer, Logger: nil, Test: nil,
		})
	}
}

// newProcessMockService configures one initialized service that waits for stream closure.
func newProcessMockService(controller *gomock.Controller) *MockService {
	service := NewMockService(controller)
	initializationOperation := NewMockInitializeOperation(controller)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ *Host) error {
		<-ctx.Done()
		return ctx.Err()
	})
	service.EXPECT().Close().Return(nil)
	return service
}

// TestConnectAndServe proves handshake, initialization lifecycle, and process cleanup.
func TestConnectAndServe(t *testing.T) {
	t.Parallel()
	// Arrange command for Connect to verify handshake, initialization lifecycle, and process cleanup.

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), uiSDKHelperEnvironment+"=serve")
	// Act by invoking Connect to exercise handshake, initialization lifecycle, and process cleanup.
	client, err := Connect(t.Context(), command)
	// Assert handshake, initialization lifecycle, and process cleanup.
	require.NoError(t, err)
	t.Cleanup(client.Close)
	assert.Equal(t, ProtocolVersion, client.NegotiatedVersion())

	stream, err := client.Service().Open(t.Context())
	require.NoError(t, err)
	sendProcessInitialization(t, stream)
	for range 3 {
		response, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		assert.Equal(t, "initialize", response.GetOperationId())
	}
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)

	client.Close()
	assert.True(t, client.Exited())
	select {
	case <-client.Done():
	default:
		assert.Fail(t, "UI plugin lifecycle signal remained open after close")
	}
}

// TestConnectRejectsVersionMismatch verifies the UI handshake has no compatibility path.
func TestConnectRejectsVersionMismatch(t *testing.T) {
	t.Parallel()
	// Arrange a child process that exposes only an unsupported protocol version.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), uiSDKHelperEnvironment+"=version-mismatch")

	// Act by connecting to the process with the unsupported handshake.
	client, err := Connect(t.Context(), command)

	// Assert negotiation fails without returning a compatibility client.
	require.Error(t, err)
	assert.Nil(t, client)
}

// TestConnectRejectsCanceledContext verifies startup honors preexisting cancellation.
func TestConnectRejectsCanceledContext(t *testing.T) {
	t.Parallel()
	// Arrange ctx, cancel, and command for Connect to verify startup honors preexisting cancellation.

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	// Act by invoking Connect to exercise startup honors preexisting cancellation.
	client, err := Connect(ctx, command)
	// Assert startup honors preexisting cancellation.
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, client)
}

// TestOpenCancellationPropagatesToPersistentStream verifies context-owned stream cancellation.
func TestOpenCancellationPropagatesToPersistentStream(t *testing.T) {
	t.Parallel()
	// Arrange controller, service, and cleanupCompleted for Client.Open to verify context-owned stream cancellation.

	controller := gomock.NewController(t)
	service := NewMockService(controller)
	cleanupCompleted := make(chan struct{})
	service.EXPECT().Close().DoAndReturn(func() error {
		close(cleanupCompleted)
		return nil
	})
	client := TestClient(t, service)
	ctx, cancel := context.WithCancel(t.Context())
	// Act by invoking Client.Open to exercise context-owned stream cancellation.
	stream, err := client.Open(ctx)
	// Assert context-owned stream cancellation.
	require.NoError(t, err)
	cancel()
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.Canceled, status.Code(err))
	require.Eventually(t, func() bool {
		select {
		case <-cleanupCompleted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

// sendProcessInitialization sends one valid Host initialization operation.
func sendProcessInitialization(t *testing.T, stream uiv1.UIService_OpenClient) {
	t.Helper()
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
}
