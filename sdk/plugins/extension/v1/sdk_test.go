package extensionv1

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const sdkHelperEnvironment = "GLYPH_EXTENSION_SDK_HELPER"

// contractService is the minimal real gRPC implementation used to verify SDK process wiring.
type contractService struct {
	extensionpb.UnimplementedExtensionServiceServer
}

// TestSDKHelperProcess runs the extension server only in the child process created by the SDK test.
func TestSDKHelperProcess(t *testing.T) {
	t.Parallel()

	switch os.Getenv(sdkHelperEnvironment) {
	case "serve":
		Serve(&contractService{
			UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
		})
	case "version-2":
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: handshakeConfig(),
			TLSProvider:     nil,
			Plugins:         nil,
			VersionedPlugins: map[int]plugin.PluginSet{
				2: {
					pluginName: &grpcExtensionPlugin{
						NetRPCUnsupportedPlugin: plugin.NetRPCUnsupportedPlugin{},
						server: &contractService{
							UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
						},
					},
				},
			},
			GRPCServer: plugin.DefaultGRPCServer,
			Logger:     nil,
			Test:       nil,
		})
	}
}

// TestConnectAndServe verifies handshake negotiation, generated-contract access, streaming, and clean shutdown.
func TestConnectAndServe(t *testing.T) {
	t.Parallel()

	// Arrange: configure this test executable to become an extension child process.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), sdkHelperEnvironment+"=serve")

	// Act: complete the real go-plugin handshake and call the generated contract.
	client, err := Connect(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	assert.Equal(t, ProtocolVersion, client.NegotiatedVersion())

	catalog, err := client.Service().ListTools(t.Context(), &extensionpb.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, catalog.GetTools(), 1)
	assert.Equal(t, "contract", catalog.GetTools()[0].GetName())

	stream, err := client.Service().Execute(t.Context(), &extensionpb.ExecuteRequest{
		ToolName:      "contract",
		ArgumentsJson: nil,
	})
	require.NoError(t, err)
	progress, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "started", progress.GetProgress().GetContent())
	terminal, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "done", terminal.GetResult().GetContent())
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)

	// Assert: closing waits until go-plugin observes child-process termination and closes the lifecycle signal.
	client.Close()
	assert.True(t, client.Exited())
	select {
	case <-client.Done():
	default:
		assert.Fail(t, "client lifecycle signal remained open after process exit")
	}
}

// ListTools returns one descriptor to prove generated unary contract access.
func (s *contractService) ListTools(
	_ context.Context,
	_ *extensionpb.ListToolsRequest,
) (*extensionpb.ListToolsResponse, error) {
	return &extensionpb.ListToolsResponse{
		Tools: []*extensionpb.ToolDescriptor{
			{Name: "contract", Description: "Contract test tool.", InputSchemaJson: []byte(`{}`)},
		},
	}, nil
}

// Execute sends progress and one terminal result to prove generated streaming contract access.
func (s *contractService) Execute(
	_ *extensionpb.ExecuteRequest,
	stream extensionpb.ExtensionService_ExecuteServer,
) error {
	if err := stream.Send(&extensionpb.ExecuteResponse{
		Content: &extensionpb.ExecuteResponse_Progress{
			Progress: &extensionpb.ToolProgress{
				Channel: extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS,
				Content: "started",
			},
		},
	}); err != nil {
		return err
	}
	if err := stream.Send(&extensionpb.ExecuteResponse{
		Content: &extensionpb.ExecuteResponse_Result{
			Result: &extensionpb.ToolResult{Content: "done", IsError: false},
		},
	}); err != nil {
		return err
	}
	return nil
}

// TestConnectRejectsVersionMismatch verifies that negotiation has no compatibility path outside protocol version 1.
func TestConnectRejectsVersionMismatch(t *testing.T) {
	t.Parallel()

	// Arrange: configure a real child process that offers only extension protocol version 2.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), sdkHelperEnvironment+"=version-2")

	// Act: attempt the go-plugin handshake through the production SDK.
	client, err := Connect(t.Context(), command)

	// Assert: reject the incompatible process and return no connected client.
	assert.Nil(t, client)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "incompatible api version")
}

// TestConnectRejectsCanceledContext verifies that process startup does not ignore prior cancellation.
func TestConnectRejectsCanceledContext(t *testing.T) {
	t.Parallel()

	// Arrange: cancel before any process may be started.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act: attempt to connect with the canceled context.
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSDKHelperProcess$")
	client, err := Connect(ctx, command)

	// Assert: no client is returned and cancellation remains identifiable.
	assert.Nil(t, client)
	require.ErrorIs(t, err, context.Canceled)
}
