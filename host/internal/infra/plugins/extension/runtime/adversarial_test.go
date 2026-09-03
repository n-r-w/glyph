//go:build integration

package runtime

import (
	"context"
	"errors"
	"os"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// adversarialPluginName is the Extension Contract go-plugin dispense name.
	adversarialPluginName = "extension"
	// adversarialCookieKey is the Extension Contract handshake key.
	adversarialCookieKey = "GLYPH_EXTENSION_PLUGIN"
	// adversarialCookieValue is the Extension Contract v1 handshake value.
	adversarialCookieValue = "glyph-extension-v1"
)

// adversarialPlugin registers a direct generated server for malformed public-protocol tests.
type adversarialPlugin struct {
	// NetRPCUnsupportedPlugin disables unsupported net/rpc transport.
	plugin.NetRPCUnsupportedPlugin
	// server emits one selected malformed lifecycle sequence.
	server extensionpb.ExtensionServiceServer
}

var (
	_ plugin.Plugin     = (*adversarialPlugin)(nil)
	_ plugin.GRPCPlugin = (*adversarialPlugin)(nil)
)

// GRPCServer registers the direct generated Extension server.
func (p *adversarialPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	extensionpb.RegisterExtensionServiceServer(server, p.server)
	return nil
}

// GRPCClient is unused because this test plugin runs only in the child process.
func (p *adversarialPlugin) GRPCClient(
	context.Context,
	*plugin.GRPCBroker,
	*grpc.ClientConn,
) (any, error) {
	return nil, errors.New("adversarial extension client is not available")
}

// adversarialServer emits malformed lifecycle sequences without using SDK validation.
type adversarialServer struct {
	// UnimplementedExtensionServiceServer provides generated forward defaults.
	extensionpb.UnimplementedExtensionServiceServer
	// mode selects one malformed sequence.
	mode string
}

var _ extensionpb.ExtensionServiceServer = (*adversarialServer)(nil)

// Open completes registration and then emits one malformed Execute lifecycle.
func (s *adversarialServer) Open(stream extensionpb.ExtensionService_OpenServer) error {
	register, err := stream.Recv()
	if err != nil {
		return err
	}
	registerID := register.GetOperationId()
	if err = sendAdversarialLifecycle(stream, registerID, registerCompletedEvent(s.mode)); err != nil {
		return err
	}
	execute, err := stream.Recv()
	if err != nil {
		return err
	}
	id := execute.GetOperationId()
	if err = stream.Send(extensionResponse(id, acceptedExtensionEvent())); err != nil {
		return err
	}
	if err = stream.Send(extensionResponse(id, runningExtensionEvent())); err != nil {
		return err
	}

	completed := toolCompletedEvent()
	switch s.mode {
	case "cancel-transport-error", "cancel-unknown-transport-error":
		if err = stream.Send(extensionResponse(id, toolProgressEvent())); err != nil {
			return err
		}
		cancellation, receiveErr := stream.Recv()
		if receiveErr != nil {
			return receiveErr
		}
		if cancellation.GetRequest().GetCancel() == nil {
			return errors.New("expected cancellation request")
		}
		if s.mode == "cancel-unknown-transport-error" {
			return status.Error(codes.Unknown, "unknown cancellation transport failed")
		}
		return status.Error(codes.Unavailable, "cancellation transport failed")
	case "cancel-handle-transport-error", "cancel-handle-unknown-transport-error":
		writeSignalFile(os.Getenv(runtimeStartedEnvironment))
		cancellation, receiveErr := stream.Recv()
		if receiveErr != nil {
			return receiveErr
		}
		if cancellation.GetRequest().GetCancel() == nil {
			return errors.New("expected cancellation request")
		}
		if s.mode == "cancel-handle-unknown-transport-error" {
			return status.Error(codes.Unknown, "unknown cancellation transport failed")
		}
		return status.Error(codes.Unavailable, "cancellation transport failed")
	case "missing-result":
		return nil
	case "duplicate-result":
		if err = stream.Send(extensionResponse(id, completed)); err != nil {
			return err
		}
		if err = stream.Send(extensionResponse(id, completed)); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	case "event-after-result":
		if err = stream.Send(extensionResponse(id, completed)); err != nil {
			return err
		}
		if err = stream.Send(extensionResponse(id, toolProgressEvent())); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	case "empty-event":
		return stream.Send(extensionpb.OpenResponse_builder{
			OperationId: new(id), Event: new(extensionpb.ExtensionEvent),
		}.Build())
	case "empty-result":
		completedEvent := new(extensionpb.ExtensionCompleted)
		completedEvent.SetTool(extensionpb.ToolResult_builder{IsError: new(false), Contents: nil}.Build())
		event := new(extensionpb.ExtensionEvent)
		event.SetCompleted(completedEvent)
		if err = stream.Send(extensionResponse(id, event)); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	case "mismatched-handler":
		completedEvent := new(extensionpb.ExtensionCompleted)
		completedEvent.SetHandle(extensionpb.HandleResponse_builder{
			SessionBeforeTreeRequest: extensionpb.SessionBeforeTreeRequestAction_builder{}.Build(),
			SessionBeforeTreeResult:  nil, SessionTree: nil, Error: nil,
		}.Build())
		event := new(extensionpb.ExtensionEvent)
		event.SetCompleted(completedEvent)
		if err = stream.Send(extensionResponse(id, event)); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	default:
		return errors.New("unknown adversarial extension mode")
	}
}

// serveAdversarialExtension serves one malformed direct contract implementation.
func serveAdversarialExtension(mode string) {
	handshake := plugin.HandshakeConfig{
		ProtocolVersion:  extensionsdk.ProtocolVersion,
		MagicCookieKey:   adversarialCookieKey,
		MagicCookieValue: adversarialCookieValue,
	}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshake,
		TLSProvider:     nil,
		Plugins:         nil,
		VersionedPlugins: map[int]plugin.PluginSet{
			extensionsdk.ProtocolVersion: {
				adversarialPluginName: &adversarialPlugin{
					NetRPCUnsupportedPlugin: plugin.NetRPCUnsupportedPlugin{},
					server: &adversarialServer{
						UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
						mode:                                mode,
					},
				},
			},
		},
		GRPCServer: plugin.DefaultGRPCServer,
		Logger:     nil,
		Test:       nil,
	})
}

// sendAdversarialLifecycle sends Accepted, Running, and one terminal event.
func sendAdversarialLifecycle(
	stream extensionpb.ExtensionService_OpenServer,
	id string,
	terminal *extensionpb.ExtensionEvent,
) error {
	for _, event := range []*extensionpb.ExtensionEvent{
		acceptedExtensionEvent(), runningExtensionEvent(), terminal,
	} {
		if err := stream.Send(extensionResponse(id, event)); err != nil {
			return err
		}
	}
	return nil
}

// extensionResponse wraps one operation event.
func extensionResponse(id string, event *extensionpb.ExtensionEvent) *extensionpb.OpenResponse {
	return extensionpb.OpenResponse_builder{OperationId: new(id), Event: event}.Build()
}

// acceptedExtensionEvent constructs Accepted.
func acceptedExtensionEvent() *extensionpb.ExtensionEvent {
	event := new(extensionpb.ExtensionEvent)
	event.SetAccepted(new(operationpb.Accepted))
	return event
}

// runningExtensionEvent constructs Running.
func runningExtensionEvent() *extensionpb.ExtensionEvent {
	event := new(extensionpb.ExtensionEvent)
	event.SetRunning(new(operationpb.Running))
	return event
}

// registerCompletedEvent constructs one valid registration completion for the selected operation kind.
func registerCompletedEvent(mode string) *extensionpb.ExtensionEvent {
	tools := []*extensionpb.ToolDescriptor{extensionpb.ToolDescriptor_builder{
		Name: new("read"), Description: new("Read a project file."),
		InputSchemaJson: []byte(validSchemaJSON), ConstrainedSampling: nil,
	}.Build()}
	var handlers []*extensionpb.HandlerDescriptor
	if mode == "mismatched-handler" || mode == "cancel-handle-transport-error" ||
		mode == "cancel-handle-unknown-transport-error" {
		tools = nil
		handlers = []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
			Id: new("observer"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
		}.Build()}
	}
	completed := new(extensionpb.ExtensionCompleted)
	completed.SetRegister(extensionpb.RegisterResponse_builder{Tools: tools, Handlers: handlers}.Build())
	event := new(extensionpb.ExtensionEvent)
	event.SetCompleted(completed)
	return event
}

// toolCompletedEvent constructs one valid Execute completion event.
func toolCompletedEvent() *extensionpb.ExtensionEvent {
	//nolint:exhaustruct_v5 // The content builder sets only text.
	content := extensionpb.ToolResultContent_builder{Text: new("done")}.Build()
	completed := new(extensionpb.ExtensionCompleted)
	completed.SetTool(extensionpb.ToolResult_builder{
		IsError: new(false), Contents: []*extensionpb.ToolResultContent{content},
	}.Build())
	event := new(extensionpb.ExtensionEvent)
	event.SetCompleted(completed)
	return event
}

// toolProgressEvent constructs one valid tool progress event.
func toolProgressEvent() *extensionpb.ExtensionEvent {
	progress := new(extensionpb.ExtensionProgress)
	progress.SetTool(extensionpb.ToolProgress_builder{
		Channel: new(extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS), Content: new("late"),
	}.Build())
	event := new(extensionpb.ExtensionEvent)
	event.SetProgress(progress)
	return event
}
