package uiv1

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const uiSDKHelperEnvironment = "GLYPH_UI_SDK_HELPER"

// contractService provides deterministic capabilities and one bidirectional exchange.
type contractService struct {
	uipb.UnimplementedUIServiceServer
}

// TestSDKHelperProcess serves the UI contract when this test binary is started as a plugin child.
func TestSDKHelperProcess(t *testing.T) {
	t.Parallel()

	switch os.Getenv(uiSDKHelperEnvironment) {
	case "serve":
		Serve(&contractService{
			UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		})
	case "version-2":
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: handshakeConfig(),
			TLSProvider:     nil,
			Plugins:         nil,
			VersionedPlugins: map[int]plugin.PluginSet{
				2: pluginSets(&contractService{
					UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
				})[ProtocolVersion],
			},
			GRPCServer: plugin.DefaultGRPCServer,
			Logger:     nil,
			Test:       nil,
		})
	}
}

// TestConnectAndServe proves handshake, capability retrieval, stream order, and process cleanup.
func TestConnectAndServe(t *testing.T) {
	t.Parallel()

	// Arrange: configure this test executable to serve as one UI plugin process.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), uiSDKHelperEnvironment+"=serve")

	// Act: connect through go-plugin and exchange both stream directions.
	client, err := Connect(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	assert.Equal(t, ProtocolVersion, client.NegotiatedVersion())
	assert.True(t, client.Capabilities().GetControlsTerminal())

	stream, err := client.Service().Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(&uipb.OpenRequest{Content: &uipb.OpenRequest_Initialization{
		Initialization: &uipb.Initialization{
			SelectedUiId: "contract", StartupContent: nil, Extensions: nil,
			Availability: uipb.Availability_AVAILABILITY_UNSPECIFIED,
		},
	}}))
	commandFrame, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "first request", commandFrame.GetSubmit().GetText())
	require.NoError(t, stream.Send(&uipb.OpenRequest{Content: &uipb.OpenRequest_Information{
		Information: &uipb.Information{Text: "received"},
	}}))
	commandFrame, err = stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, commandFrame.GetQuit())
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)

	// Assert: client shutdown observes and exposes child-process termination.
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

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), uiSDKHelperEnvironment+"=version-2")

	client, err := Connect(t.Context(), command)

	require.Error(t, err)
	assert.Nil(t, client)
}

// TestConnectRejectsCanceledContext verifies startup honors preexisting cancellation.
func TestConnectRejectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")

	client, err := Connect(ctx, command)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, client)
}

// TestClientProvidesGeneratedContractAccess verifies the public contract-test helper.
func TestClientProvidesGeneratedContractAccess(t *testing.T) {
	t.Parallel()

	client := TestClient(t, &contractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
	})

	capabilities, err := client.GetCapabilities(t.Context(), &uipb.GetCapabilitiesRequest{})
	require.NoError(t, err)
	assert.True(t, capabilities.GetControlsTerminal())
}

// TestOpenCancellationPropagatesToThePersistentStream verifies context-owned stream cancellation.
func TestOpenCancellationPropagatesToThePersistentStream(t *testing.T) {
	t.Parallel()

	client := TestClient(t, &contractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
	})
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.Open(ctx)
	require.NoError(t, err)

	cancel()
	_, err = stream.Recv()

	require.Error(t, err)
	assert.Equal(t, codes.Canceled, status.Code(err))
}

// GetCapabilities returns the fixed terminal-control capability used by the contract test.
func (*contractService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	return &uipb.GetCapabilitiesResponse{ControlsTerminal: true}, nil
}

// Open validates Host frame order and returns one submit followed by quit.
func (*contractService) Open(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	initialization, err := stream.Recv()
	if err != nil {
		return err
	}
	if initialization.GetInitialization().GetSelectedUiId() != "contract" {
		return nil
	}
	if err := stream.Send(&uipb.OpenResponse{Content: &uipb.OpenResponse_Submit{
		Submit: &uipb.SubmitCommand{Text: "first request"},
	}}); err != nil {
		return err
	}
	if _, err := stream.Recv(); err != nil {
		return err
	}
	return stream.Send(&uipb.OpenResponse{Content: &uipb.OpenResponse_Quit{Quit: &uipb.QuitCommand{}}})
}
