// Package uiv1 owns UI Plugin Contract process and operation lifecycles.
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

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// ProtocolVersion is the only supported UI plugin protocol version.
	ProtocolVersion  = 1
	pluginName       = "glyph-ui"
	magicCookieKey   = "GLYPH_UI_PLUGIN"
	magicCookieValue = "glyph-ui-v1"
	startTimeout     = 10 * time.Second
)

// Client owns one UI plugin process connection.
type Client struct {
	// process owns the go-plugin process connection.
	process *plugin.Client
	// service is the generated UI gRPC client.
	service uiv1.UIServiceClient
	// done closes when the UI process exits.
	done <-chan struct{}
	// version is the negotiated protocol version.
	version int
	// closeOnce limits process closure to one attempt.
	closeOnce sync.Once
}

// grpcUIPlugin adapts the SDK-owned service to go-plugin.
type grpcUIPlugin struct {
	// NetRPCUnsupportedPlugin disables unsupported net/rpc transport.
	plugin.NetRPCUnsupportedPlugin
	// server implements the generated UI service.
	server uiv1.UIServiceServer
}

// uiGRPCClient retains the generated client and go-plugin lifecycle signal.
type uiGRPCClient struct {
	// service is the generated UI gRPC client.
	service uiv1.UIServiceClient
	// done closes when the UI process exits.
	done <-chan struct{}
}

// Connect starts one process and negotiates the required protocol.
func Connect(ctx context.Context, command *exec.Cmd) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("connect UI plugin: %w", err)
	}
	if command == nil {
		return nil, errors.New("connect UI plugin: command is required")
	}
	process := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: handshakeConfig(), Plugins: nil, VersionedPlugins: pluginSets(nil), Cmd: command,
		Reattach: nil, RunnerFunc: nil, SecureConfig: nil, TLSConfig: nil, Managed: false, MinPort: 0, MaxPort: 0,
		StartTimeout: startTimeout, Stderr: nil, SyncStdout: nil, SyncStderr: nil,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC}, Logger: hclog.NewNullLogger(),
		PluginLogBufferSize: 0, AutoMTLS: false, GRPCDialOptions: nil, GRPCBrokerMultiplex: false,
		SkipHostEnv: false, UnixSocketConfig: nil,
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
	return &Client{
		process: process, service: adapter.service, done: adapter.done,
		version: process.NegotiatedVersion(), closeOnce: sync.Once{},
	}, nil
}

// Service exposes the generated UI contract client to the Host.
func (c *Client) Service() uiv1.UIServiceClient { return c.service }

// NegotiatedVersion returns the selected protocol version.
func (c *Client) NegotiatedVersion() int { return c.version }

// Exited reports whether go-plugin observed child-process termination.
func (c *Client) Exited() bool { return c.process.Exited() }

// Done closes after the UI plugin process exits.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close stops the UI plugin process once.
func (c *Client) Close() { c.closeOnce.Do(c.process.Kill) }

// Serve starts the plugin-side SDK-owned gRPC server.
func Serve(service Service) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig(), TLSProvider: nil, Plugins: nil,
		VersionedPlugins: pluginSets(newServer(service)), GRPCServer: plugin.DefaultGRPCServer, Logger: nil, Test: nil,
	})
}

// TestClient connects a generated client to one SDK-owned service for contract tests.
func TestClient(t testing.TB, service Service) uiv1.UIServiceClient {
	t.Helper()
	client, _ := plugin.TestPluginGRPCConn(t, false, pluginSets(newServer(service))[ProtocolVersion])
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

// GRPCServer registers the SDK-owned UI implementation.
func (p *grpcUIPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	if p.server == nil {
		return errors.New("register UI service: server is required")
	}
	uiv1.RegisterUIServiceServer(server, p.server)
	return nil
}

// GRPCClient creates the generated UI client in the Host process.
func (*grpcUIPlugin) GRPCClient(ctx context.Context, _ *plugin.GRPCBroker, connection *grpc.ClientConn) (any, error) {
	return &uiGRPCClient{service: uiv1.NewUIServiceClient(connection), done: ctx.Done()}, nil
}

// handshakeConfig returns an isolated copy of the UI Contract handshake.
func handshakeConfig() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion: ProtocolVersion, MagicCookieKey: magicCookieKey,
		MagicCookieValue: magicCookieValue,
	}
}

// pluginSets restricts negotiation to the required UI Contract protocol.
func pluginSets(server uiv1.UIServiceServer) map[int]plugin.PluginSet {
	return map[int]plugin.PluginSet{ProtocolVersion: {pluginName: &grpcUIPlugin{
		NetRPCUnsupportedPlugin: plugin.NetRPCUnsupportedPlugin{}, server: server,
	}}}
}
