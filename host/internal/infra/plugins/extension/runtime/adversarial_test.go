//go:build integration

package runtime

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

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

const (
	// shutdownHostWorkMode selects a malformed invocation result after Host admission.
	shutdownHostWorkMode = "shutdown-host-work"
	// shutdownHostWorkActiveMode keeps the invocation active until owner close.
	shutdownHostWorkActiveMode = "shutdown-host-work-active"
	// shutdownHostWorkLossMode selects transport loss with peer-owned classification text.
	shutdownHostWorkLossMode = "shutdown-host-work-loss"
	// shutdownHostWorkCancelMode selects tool progress followed by caller cancellation.
	shutdownHostWorkCancelMode = "shutdown-host-work-cancel"
	// shutdownHostWorkCancelHandlerMode selects handler cancellation after Host admission.
	shutdownHostWorkCancelHandlerMode = "shutdown-host-work-cancel-handler"
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

// Open serves the selected direct protocol fixture scenario.
func (s *adversarialServer) Open(stream extensionpb.ExtensionService_OpenServer) error {
	// shutdownHostWorkPrefix identifies the protocol family with admitted nested Host work.
	const shutdownHostWorkPrefix = "shutdown-host-work"

	register, err := stream.Recv()
	if err != nil {
		return err
	}
	registerID := register.GetOperationId()
	if s.mode != "host-rejection-registration" {
		if err = sendAdversarialLifecycle(stream, registerID, registerCompletedEvent(s.mode)); err != nil {
			return err
		}
	}
	if s.mode == "host-rejection-delivered" {
		request := new(extensionpb.ExtensionRequest)
		request.SetGetModels(new(extensionpb.GetModelsRequest))
		if err = stream.Send(extensionpb.OpenResponse_builder{
			OperationId: new("delivered-rejection"), Request: request, Event: nil,
		}.Build()); err != nil {
			return err
		}
		response, receiveErr := stream.Recv()
		if receiveErr != nil {
			return receiveErr
		}
		if response.GetEvent().GetRejected() == nil {
			return errors.New("expected Host rejection")
		}
		os.Exit(23)
	}
	if s.mode == "host-rejection-completion" || s.mode == "host-rejection-registration" {
		// Fill the existing outbound flow-control window without consuming Host rejection output.
		padding := strings.Repeat("x", 1<<20)
		for index := range 128 {
			request := new(extensionpb.ExtensionRequest)
			request.SetGetModels(new(extensionpb.GetModelsRequest))
			if sendErr := stream.Send(extensionpb.OpenResponse_builder{
				OperationId: new(strconv.Itoa(index) + padding), Request: request, Event: nil,
			}.Build()); sendErr != nil {
				os.Exit(23)
			}
		}
		<-stream.Context().Done()
		os.Exit(23)
	}
	execute, err := stream.Recv()
	if err != nil {
		return err
	}
	id := execute.GetOperationId()
	switch s.mode {
	case "failure-before-accepted":
		if err = stream.Send(extensionResponse(id, failedExtensionEvent(
			"INTERNAL", "complete failure before Accepted text",
		))); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	case "unknown-operation-rejection":
		if err = stream.Send(extensionResponse("unknown", rejectedExtensionEvent(
			"BUSY", "complete unknown operation rejection text",
		))); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	case "unknown-operation-failure":
		if err = stream.Send(extensionResponse("unknown", failedExtensionEvent(
			"INTERNAL", "complete unknown operation failure text",
		))); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	}
	if s.mode == "unsupported-rejection" {
		if err = stream.Send(extensionResponse(id, rejectedExtensionEvent(
			"UNSUPPORTED", "complete peer rejection text",
		))); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
	}
	if err = stream.Send(extensionResponse(id, acceptedExtensionEvent())); err != nil {
		return err
	}
	if err = stream.Send(extensionResponse(id, runningExtensionEvent())); err != nil {
		return err
	}

	if strings.HasPrefix(s.mode, shutdownHostWorkPrefix) {
		return s.serveShutdownHostWork(stream, execute)
	}
	completed := toolCompletedEvent()
	switch s.mode {
	case "unsupported-failure":
		if err = stream.Send(extensionResponse(id, failedExtensionEvent(
			"UNSUPPORTED", "complete peer failure text",
		))); err != nil {
			return err
		}
		<-stream.Context().Done()
		return context.Cause(stream.Context())
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
			Request: nil,

			OperationId: new(id), Event: new(extensionpb.ExtensionEvent),
		}.Build())
	case "empty-result":
		return sendEmptyToolCompletion(stream, id)
	case "mismatched-handler":
		completedEvent := new(extensionpb.ExtensionCompleted)
		completedEvent.SetHandle(extensionpb.HandleResponse_builder{
			CompactionRequest: nil, CompactionGenerate: nil, CompactionResult: nil,
			CompactionSuccess: nil, CompactionFailure: nil,
			Retry:          nil,
			ModelSelection: nil, ReasoningSelection: nil,
			Lifecycle:                nil,
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

// serveShutdownHostWork drives admitted Host work through invocation failure or owner close.
func (s *adversarialServer) serveShutdownHostWork(
	stream extensionpb.ExtensionService_OpenServer, execute *extensionpb.OpenRequest,
) error {
	// nestedHostWorkID identifies the Host operation admitted before invocation failure.
	const nestedHostWorkID = "nested-host-work"

	// id identifies the public invocation in shutdown protocol output.
	id := execute.GetOperationId()
	// request contains the nested Host operation required for the shutdown scenario.
	request := new(extensionpb.ExtensionRequest)
	// binding holds the caller context identity for nested Host admission.
	binding := execute.GetRequest().GetExecute().GetContext()
	if execute.GetRequest().GetHandle() != nil {
		binding = execute.GetRequest().GetHandle().GetContext()
	}
	request.SetGetModels(extensionpb.GetModelsRequest_builder{
		Context: extensionpb.ExtensionContextRef_builder{
			ContextId: new(binding.GetContextId()), RuntimeInstanceId: new(binding.GetRuntimeInstanceId()),
			SessionId: new(binding.GetSessionId()),
		}.Build(),
	}.Build())
	if err := stream.Send(extensionpb.OpenResponse_builder{
		OperationId: new(nestedHostWorkID), Request: request, Event: nil,
	}.Build()); err != nil {
		return err
	}
	// Wait for admission before failing the invocation that shares this connection.
	for {
		// response contains the Host admission acknowledgment.
		response, receiveErr := stream.Recv()
		if receiveErr != nil {
			return receiveErr
		}
		if response.GetEvent().GetRunning() != nil {
			break
		}
	}
	if s.mode == shutdownHostWorkActiveMode {
		if err := stream.Send(extensionResponse(id, toolProgressEvent())); err != nil {
			return err
		}
		for {
			// response contains the owner Close request.
			response, receiveErr := stream.Recv()
			if receiveErr != nil {
				return receiveErr
			}
			if response.GetClose() != nil {
				return status.Error(codes.Unavailable, "complete runtime close transport failure")
			}
		}
	}
	if s.mode == shutdownHostWorkLossMode {
		return status.Error(codes.Unavailable, invocationTransportFailureText)
	}
	if s.mode == shutdownHostWorkCancelMode {
		if err := stream.Send(extensionResponse(id, toolProgressEvent())); err != nil {
			return err
		}
	}
	if s.mode == shutdownHostWorkCancelMode || s.mode == shutdownHostWorkCancelHandlerMode {
		// cancellation contains the caller request expected after Host admission.
		cancellation, receiveErr := stream.Recv()
		if receiveErr != nil {
			return receiveErr
		}
		if cancellation.GetRequest().GetCancel() == nil {
			return errors.New("expected cancellation request")
		}
		return status.Error(codes.Unavailable, "complete invocation transport failure")
	}
	return sendEmptyToolCompletion(stream, id)
}

// sendEmptyToolCompletion supplies a malformed tool completion for protocol-failure tests.
func sendEmptyToolCompletion(stream extensionpb.ExtensionService_OpenServer, id string) error {
	// completedEvent holds the malformed tool result used to fail the invocation.
	completedEvent := new(extensionpb.ExtensionCompleted)
	completedEvent.SetTool(extensionpb.ToolResult_builder{IsError: new(false), Contents: nil}.Build())
	// event contains the malformed completion for the public protocol.
	event := new(extensionpb.ExtensionEvent)
	event.SetCompleted(completedEvent)
	if err := stream.Send(extensionResponse(id, event)); err != nil {
		return err
	}
	// Keep the peer stream alive so Host-owned shutdown controls its lifetime.
	<-stream.Context().Done()
	return context.Cause(stream.Context())
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
	return extensionpb.OpenResponse_builder{
		Request:     nil,
		OperationId: new(id), Event: event,
	}.Build()
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

// rejectedExtensionEvent returns a peer rejection with the selected category and complete text.
func rejectedExtensionEvent(code string, message string) *extensionpb.ExtensionEvent {
	event := new(extensionpb.ExtensionEvent)
	event.SetRejected(operationpb.Rejected_builder{Code: new(code), Message: new(message)}.Build())
	return event
}

// failedExtensionEvent returns a peer failure with the selected category and complete text.
func failedExtensionEvent(code string, message string) *extensionpb.ExtensionEvent {
	event := new(extensionpb.ExtensionEvent)
	event.SetFailed(operationpb.Failed_builder{Code: new(code), Message: new(message)}.Build())
	return event
}

// registerCompletedEvent constructs one valid registration completion for the selected operation kind.
func registerCompletedEvent(mode string) *extensionpb.ExtensionEvent {
	tools := []*extensionpb.ToolDescriptor{extensionpb.ToolDescriptor_builder{
		Name: new(fixtureToolName), Description: new("Read a project file."),
		InputSchemaJson: []byte(validSchemaJSON), ConstrainedSampling: nil,
	}.Build()}
	var handlers []*extensionpb.HandlerDescriptor
	if mode == "mismatched-handler" || mode == "cancel-handle-transport-error" ||
		mode == "cancel-handle-unknown-transport-error" {
		tools = nil
		handlers = []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
			Id: new(fixtureObserverID), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
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
