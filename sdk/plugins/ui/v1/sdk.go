// Package uiv1 bootstraps UI Plugin Contract v1 processes and gRPC connections.
package uiv1

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// ProtocolVersion is the only supported UI plugin protocol version.
	ProtocolVersion  = 1
	pluginName       = "glyph-ui"
	magicCookieKey   = "GLYPH_UI_PLUGIN"
	magicCookieValue = "glyph-ui-v1"
	startTimeout     = 10 * time.Second
)

// Client owns one UI plugin process connection and its fixed capabilities.
type Client struct {
	// process owns the go-plugin process connection.
	process *plugin.Client
	// service is the generated UI gRPC client.
	service uipb.UIServiceClient
	// capabilities contains immutable UI startup behavior.
	capabilities *uipb.GetCapabilitiesResponse
	// done closes when the UI process exits.
	done <-chan struct{}
	// version is the negotiated protocol version.
	version int
	// closeOnce limits process closure to one attempt.
	closeOnce sync.Once
}

// grpcUIPlugin adapts the generated UI service to go-plugin.
type grpcUIPlugin struct {
	// NetRPCUnsupportedPlugin disables unsupported net/rpc transport.
	plugin.NetRPCUnsupportedPlugin
	// server implements the generated UI service.
	server uipb.UIServiceServer
}

// uiGRPCClient retains the generated client and go-plugin lifecycle signal.
type uiGRPCClient struct {
	// service is the generated UI gRPC client.
	service uipb.UIServiceClient
	// done closes when the UI process exits.
	done <-chan struct{}
}

// Connect starts one process, negotiates protocol v1, and retrieves fixed capabilities.
func Connect(ctx context.Context, command *exec.Cmd) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("connect UI plugin: %w", err)
	}
	if command == nil {
		return nil, errors.New("connect UI plugin: command is required")
	}

	process := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:     handshakeConfig(),
		Plugins:             nil,
		VersionedPlugins:    pluginSets(nil),
		Cmd:                 command,
		Reattach:            nil,
		RunnerFunc:          nil,
		SecureConfig:        nil,
		TLSConfig:           nil,
		Managed:             false,
		MinPort:             0,
		MaxPort:             0,
		StartTimeout:        startTimeout,
		Stderr:              nil,
		SyncStdout:          nil,
		SyncStderr:          nil,
		AllowedProtocols:    []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:              hclog.NewNullLogger(),
		PluginLogBufferSize: 0,
		AutoMTLS:            false,
		GRPCDialOptions:     nil,
		GRPCBrokerMultiplex: false,
		SkipHostEnv:         false,
		UnixSocketConfig:    nil,
	})
	protocolClient, err := process.Client()
	if err != nil {
		process.Kill()
		return nil, fmt.Errorf("connect UI plugin process: %w", err)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		process.Kill()
		return nil, fmt.Errorf("connect UI plugin: %w", contextErr)
	}
	if process.NegotiatedVersion() != ProtocolVersion {
		process.Kill()
		return nil, fmt.Errorf("connect UI plugin: negotiated protocol version %d", process.NegotiatedVersion())
	}

	dispensed, err := protocolClient.Dispense(pluginName)
	if err != nil {
		process.Kill()
		return nil, fmt.Errorf("dispense UI contract: %w", err)
	}
	adapter, ok := dispensed.(*uiGRPCClient)
	if !ok {
		process.Kill()
		return nil, fmt.Errorf("dispense UI contract: unexpected client %T", dispensed)
	}
	capabilities, err := adapter.service.GetCapabilities(ctx, &uipb.GetCapabilitiesRequest{})
	if err != nil {
		process.Kill()
		return nil, fmt.Errorf("retrieve UI capabilities: %w", err)
	}
	return &Client{
		process:      process,
		service:      adapter.service,
		capabilities: capabilities,
		done:         adapter.done,
		version:      process.NegotiatedVersion(),
		closeOnce:    sync.Once{},
	}, nil
}

// Service exposes the generated UI contract client.
func (c *Client) Service() uipb.UIServiceClient {
	return c.service
}

// Capabilities returns the fixed startup capabilities retrieved during connection.
func (c *Client) Capabilities() *uipb.GetCapabilitiesResponse {
	return c.capabilities
}

// NegotiatedVersion returns the go-plugin protocol version selected at startup.
func (c *Client) NegotiatedVersion() int {
	return c.version
}

// Exited reports whether go-plugin observed child-process termination.
func (c *Client) Exited() bool {
	return c.process.Exited()
}

// Done closes after the UI plugin process exits.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close stops the UI plugin process once.
func (c *Client) Close() {
	c.closeOnce.Do(c.process.Kill)
}

// Serve starts the plugin-side gRPC server.
func Serve(server uipb.UIServiceServer) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig:  handshakeConfig(),
		TLSProvider:      nil,
		Plugins:          nil,
		VersionedPlugins: pluginSets(server),
		GRPCServer:       plugin.DefaultGRPCServer,
		Logger:           nil,
		Test:             nil,
	})
}

// TestClient connects a generated client and server through go-plugin's contract test transport.
func TestClient(t testing.TB, server uipb.UIServiceServer) uipb.UIServiceClient {
	t.Helper()
	client, _ := plugin.TestPluginGRPCConn(t, false, pluginSets(server)[ProtocolVersion])
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close UI contract test client: %v", err)
		}
	})
	dispensed, err := client.Dispense(pluginName)
	if err != nil {
		t.Fatalf("dispense UI contract test client: %v", err)
	}
	adapter, ok := dispensed.(*uiGRPCClient)
	if !ok {
		t.Fatalf("dispense UI contract test client: unexpected client %T", dispensed)
	}
	return adapter.service
}

// GRPCServer registers the UI implementation in the plugin process.
func (p *grpcUIPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	if p.server == nil {
		return errors.New("register UI service: server is required")
	}
	uipb.RegisterUIServiceServer(server, p.server)
	return nil
}

// GRPCClient creates the generated UI client in the Host process.
func (*grpcUIPlugin) GRPCClient(
	ctx context.Context,
	_ *plugin.GRPCBroker,
	connection *grpc.ClientConn,
) (any, error) {
	return &uiGRPCClient{service: uipb.NewUIServiceClient(connection), done: ctx.Done()}, nil
}

// handshakeConfig returns an isolated copy of the UI Contract v1 handshake.
func handshakeConfig() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion:  ProtocolVersion,
		MagicCookieKey:   magicCookieKey,
		MagicCookieValue: magicCookieValue,
	}
}

// pluginSets restricts negotiation to UI Contract v1.
func pluginSets(server uipb.UIServiceServer) map[int]plugin.PluginSet {
	return map[int]plugin.PluginSet{
		ProtocolVersion: {
			pluginName: &grpcUIPlugin{
				NetRPCUnsupportedPlugin: plugin.NetRPCUnsupportedPlugin{},
				server:                  server,
			},
		},
	}
}
