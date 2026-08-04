// Package extensionv1 provides Extension Contract v1 process bootstrap and connection support.
package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// ProtocolVersion is the only extension protocol version accepted by this SDK.
	ProtocolVersion = 1

	pluginName       = "extension"
	magicCookieKey   = "GLYPH_EXTENSION_PLUGIN"
	magicCookieValue = "glyph-extension-v1"
	startTimeout     = 10 * time.Second
)

// Client owns one connected extension process.
type Client struct {
	process   *plugin.Client
	service   extensionpb.ExtensionServiceClient
	done      <-chan struct{}
	version   int
	closeOnce sync.Once
}

// grpcExtensionPlugin binds the generated extension service to go-plugin's gRPC transport.
type grpcExtensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	server extensionpb.ExtensionServiceServer
}

// grpcClient keeps the generated contract and the process-lifecycle signal together.
type grpcClient struct {
	service extensionpb.ExtensionServiceClient
	done    <-chan struct{}
}

var (
	_ plugin.Plugin     = (*grpcExtensionPlugin)(nil)
	_ plugin.GRPCPlugin = (*grpcExtensionPlugin)(nil)
)

// Connect starts and connects to the extension process described by command.
func Connect(ctx context.Context, command *exec.Cmd) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("connect extension: %w", err)
	}
	if command == nil {
		return nil, errors.New("connect extension: command is required")
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
		return nil, fmt.Errorf("connect extension process: %w", err)
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		process.Kill()
		return nil, fmt.Errorf("connect extension: %w", contextErr)
	}
	if process.NegotiatedVersion() != ProtocolVersion {
		process.Kill()
		return nil, fmt.Errorf("connect extension: negotiated protocol version %d", process.NegotiatedVersion())
	}

	dispensed, err := protocolClient.Dispense(pluginName)
	if err != nil {
		process.Kill()
		return nil, fmt.Errorf("dispense extension contract: %w", err)
	}
	adapter, ok := dispensed.(*grpcClient)
	if !ok {
		process.Kill()
		return nil, fmt.Errorf("dispense extension contract: unexpected client %T", dispensed)
	}
	return &Client{
		process:   process,
		service:   adapter.service,
		done:      adapter.done,
		version:   process.NegotiatedVersion(),
		closeOnce: sync.Once{},
	}, nil
}

// Service returns the generated gRPC client for the connected extension.
func (c *Client) Service() extensionpb.ExtensionServiceClient {
	return c.service
}

// NegotiatedVersion returns the protocol version selected during the handshake.
func (c *Client) NegotiatedVersion() int {
	return c.version
}

// Exited reports whether the extension process has terminated.
func (c *Client) Exited() bool {
	return c.process.Exited()
}

// Done closes when the extension process terminates.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close stops the extension process and waits for process cleanup.
func (c *Client) Close() {
	c.closeOnce.Do(c.process.Kill)
}

// Serve starts the go-plugin gRPC server for an extension implementation.
func Serve(server extensionpb.ExtensionServiceServer) {
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

// GRPCServer registers the extension implementation in the plugin process.
func (p *grpcExtensionPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	if p.server == nil {
		return errors.New("register extension service: server is required")
	}
	extensionpb.RegisterExtensionServiceServer(server, p.server)
	return nil
}

// GRPCClient creates the generated extension client in the Host process.
func (p *grpcExtensionPlugin) GRPCClient(
	ctx context.Context,
	_ *plugin.GRPCBroker,
	connection *grpc.ClientConn,
) (any, error) {
	return &grpcClient{
		service: extensionpb.NewExtensionServiceClient(connection),
		done:    ctx.Done(),
	}, nil
}

// handshakeConfig returns an isolated copy of the Extension Contract v1 handshake.
func handshakeConfig() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion:  ProtocolVersion,
		MagicCookieKey:   magicCookieKey,
		MagicCookieValue: magicCookieValue,
	}
}

// pluginSets restricts negotiation to Extension Contract v1.
func pluginSets(server extensionpb.ExtensionServiceServer) map[int]plugin.PluginSet {
	return map[int]plugin.PluginSet{
		ProtocolVersion: {
			pluginName: &grpcExtensionPlugin{
				NetRPCUnsupportedPlugin: plugin.NetRPCUnsupportedPlugin{},
				server:                  server,
			},
		},
	}
}
