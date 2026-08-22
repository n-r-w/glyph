package main

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	protocolv1 "github.com/n-r-w/glyph/experiments/plugin-runtime-spike/protocol/v1"
)

const (
	protocolVersion      = 1
	extensionPluginName  = "extension"
	uiPluginName         = "ui"
	extensionCookieKey   = "GLYPH_EXTENSION_SPIKE"
	extensionCookieValue = "extension-v1"
	uiCookieKey          = "GLYPH_UI_SPIKE"
	uiCookieValue        = "ui-v1"
)

// toolOutcome is the provider-neutral terminal result observed by the Host check.
type toolOutcome struct {
	content string
	isError bool
}

// executionEvent is one provider-neutral progress or terminal execution event.
type executionEvent struct {
	progress string
	result   *toolOutcome
}

// responseReceiver is the generated stream surface consumed inside the transport adapter.
type responseReceiver interface {
	Recv() (*protocolv1.ExecuteResponse, error)
}

// executionStream prevents generated protobuf stream types from reaching Host check logic.
type executionStream struct {
	receiver responseReceiver
}

// recv converts one generated execution response into the spike's provider-neutral event.
func (s *executionStream) recv() (executionEvent, error) {
	response, err := s.receiver.Recv()
	if err != nil {
		return executionEvent{}, err
	}

	switch content := response.Content.(type) {
	case *protocolv1.ExecuteResponse_Progress:
		return executionEvent{progress: content.Progress}, nil
	case *protocolv1.ExecuteResponse_Result:
		return executionEvent{result: &toolOutcome{
			content: content.Result.Content,
			isError: content.Result.IsError,
		}}, nil
	default:
		return executionEvent{}, fmt.Errorf("receive execution event: missing content")
	}
}

// extensionRPCClient is the Host-side transport adapter for one extension process.
type extensionRPCClient struct {
	client protocolv1.ExtensionServiceClient
}

// listTools returns the extension catalog without exposing generated messages.
func (c *extensionRPCClient) listTools(ctx context.Context) ([]string, error) {
	response, err := c.client.ListTools(ctx, &protocolv1.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list extension tools: %w", err)
	}

	tools := make([]string, 0, len(response.Tools))
	for _, tool := range response.Tools {
		tools = append(tools, tool.Name)
	}
	return tools, nil
}

// execute opens one streamed extension call without exposing generated messages.
func (c *extensionRPCClient) execute(ctx context.Context, toolName, input string) (*executionStream, error) {
	stream, err := c.client.Execute(ctx, &protocolv1.ExecuteRequest{
		ToolName: toolName,
		Input:    input,
	})
	if err != nil {
		return nil, fmt.Errorf("start extension execution: %w", err)
	}
	return &executionStream{receiver: stream}, nil
}

// extensionPlugin binds the public extension gRPC service to go-plugin.
type extensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	server protocolv1.ExtensionServiceServer
}

var _ plugin.GRPCPlugin = (*extensionPlugin)(nil)

// GRPCServer registers the extension service inside the plugin process.
func (p *extensionPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	protocolv1.RegisterExtensionServiceServer(server, p.server)
	return nil
}

// GRPCClient creates the Host-side extension transport adapter.
func (_ *extensionPlugin) GRPCClient(
	_ context.Context,
	_ *plugin.GRPCBroker,
	connection *grpc.ClientConn,
) (any, error) {
	return &extensionRPCClient{client: protocolv1.NewExtensionServiceClient(connection)}, nil
}

// uiCommand is the provider-neutral Host command sent through the UI stream.
type uiCommand uint8

const (
	uiCommandEcho uiCommand = iota + 1
	uiCommandQuit
	uiCommandCrash
)

// uiEvent is the provider-neutral UI event received by the Host.
type uiEvent uint8

const (
	uiEventReady uiEvent = iota + 1
	uiEventEchoed
	uiEventExited
	uiEventResized
)

// uiMessage is one provider-neutral event from the active UI stream.
type uiMessage struct {
	event uiEvent
	text  string
}

// uiTransport is the generated bidirectional stream surface kept inside the adapter.
type uiTransport interface {
	Send(*protocolv1.OpenRequest) error
	Recv() (*protocolv1.OpenResponse, error)
}

// uiStream prevents generated protobuf stream types from reaching Host check logic.
type uiStream struct {
	transport uiTransport
}

// send converts one provider-neutral Host command to the generated contract.
func (s *uiStream) send(command uiCommand) error {
	var wireCommand protocolv1.UICommand
	switch command {
	case uiCommandEcho:
		wireCommand = protocolv1.UICommand_UI_COMMAND_ECHO
	case uiCommandQuit:
		wireCommand = protocolv1.UICommand_UI_COMMAND_QUIT
	case uiCommandCrash:
		wireCommand = protocolv1.UICommand_UI_COMMAND_CRASH
	default:
		return fmt.Errorf("send UI command: unknown command %d", command)
	}

	if err := s.transport.Send(&protocolv1.OpenRequest{Command: wireCommand}); err != nil {
		return fmt.Errorf("send UI command: %w", err)
	}
	return nil
}

// recv converts one generated UI response into a provider-neutral message.
func (s *uiStream) recv() (uiMessage, error) {
	response, err := s.transport.Recv()
	if err != nil {
		return uiMessage{}, err
	}

	var event uiEvent
	switch response.Event {
	case protocolv1.UIEvent_UI_EVENT_READY:
		event = uiEventReady
	case protocolv1.UIEvent_UI_EVENT_ECHOED:
		event = uiEventEchoed
	case protocolv1.UIEvent_UI_EVENT_EXITED:
		event = uiEventExited
	case protocolv1.UIEvent_UI_EVENT_RESIZED:
		event = uiEventResized
	default:
		return uiMessage{}, fmt.Errorf("receive UI event: unknown event %s", response.Event)
	}
	return uiMessage{event: event, text: response.Text}, nil
}

// uiRPCClient is the Host-side transport adapter for one UI plugin process.
type uiRPCClient struct {
	client protocolv1.UIServiceClient
}

// describe returns fixed UI capabilities without opening the lifecycle stream.
func (c *uiRPCClient) describe(ctx context.Context) (bool, error) {
	response, err := c.client.Describe(ctx, &protocolv1.DescribeRequest{})
	if err != nil {
		return false, fmt.Errorf("describe UI plugin: %w", err)
	}
	return response.UsesTerminal, nil
}

// open creates the single persistent UI lifecycle stream.
func (c *uiRPCClient) open(ctx context.Context) (*uiStream, error) {
	stream, err := c.client.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open UI stream: %w", err)
	}
	return &uiStream{transport: stream}, nil
}

// uiPlugin binds the public UI gRPC service to go-plugin.
type uiPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	server protocolv1.UIServiceServer
}

var _ plugin.GRPCPlugin = (*uiPlugin)(nil)

// GRPCServer registers the UI service inside the plugin process.
func (p *uiPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	protocolv1.RegisterUIServiceServer(server, p.server)
	return nil
}

// GRPCClient creates the Host-side UI transport adapter.
func (_ *uiPlugin) GRPCClient(
	_ context.Context,
	_ *plugin.GRPCBroker,
	connection *grpc.ClientConn,
) (any, error) {
	return &uiRPCClient{client: protocolv1.NewUIServiceClient(connection)}, nil
}

// extensionHandshake returns the independent extension protocol cookie.
func extensionHandshake() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion:  protocolVersion,
		MagicCookieKey:   extensionCookieKey,
		MagicCookieValue: extensionCookieValue,
	}
}

// uiHandshake returns the independent UI protocol cookie.
func uiHandshake() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion:  protocolVersion,
		MagicCookieKey:   uiCookieKey,
		MagicCookieValue: uiCookieValue,
	}
}

// extensionPluginSets exposes exactly extension protocol version 1.
func extensionPluginSets(server protocolv1.ExtensionServiceServer) map[int]plugin.PluginSet {
	return map[int]plugin.PluginSet{
		protocolVersion: {
			extensionPluginName: &extensionPlugin{server: server},
		},
	}
}

// uiPluginSets exposes exactly UI protocol version 1.
func uiPluginSets(server protocolv1.UIServiceServer) map[int]plugin.PluginSet {
	return map[int]plugin.PluginSet{
		protocolVersion: {
			uiPluginName: &uiPlugin{server: server},
		},
	}
}
