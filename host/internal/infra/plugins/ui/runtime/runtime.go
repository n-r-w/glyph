// Package runtime adapts the public UI SDK to Host UI lifecycle contracts.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"google.golang.org/protobuf/types/known/structpb"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Factory starts UI candidates through the public SDK.
type Factory struct{}

var _ hostui.RuntimeFactory = (*Factory)(nil)

// Runtime owns one connected UI process and its single stream.
type Runtime struct {
	client   *uisdk.Client
	openOnce sync.Once
	channel  hostui.Channel
	openErr  error
}

var _ hostui.Runtime = (*Runtime)(nil)

// channel maps provider-neutral frames and commands to the generated contract.
type channel struct {
	stream uipb.UIService_OpenClient
	mutex  sync.Mutex
}

var _ hostui.Channel = (*channel)(nil)

// NewFactory creates a UI runtime factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Start launches one trusted local UI candidate and validates its fixed capabilities.
func (*Factory) Start(ctx context.Context, candidate domainui.Candidate) (hostui.Runtime, error) {
	//nolint:gosec // The catalog contains trusted local UI plugin executables.
	command := exec.CommandContext(context.WithoutCancel(ctx), candidate.Path)
	client, err := uisdk.Connect(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start UI %q: %w", candidate.ID, err)
	}
	return &Runtime{client: client, openOnce: sync.Once{}, channel: nil, openErr: nil}, nil
}

// Capabilities returns the immutable capabilities retrieved before stream creation.
func (r *Runtime) Capabilities() domainui.Capabilities {
	return domainui.Capabilities{ControlsTerminal: r.client.Capabilities().GetControlsTerminal()}
}

// Open opens and reuses the one persistent UI lifecycle stream.
func (r *Runtime) Open(ctx context.Context) (hostui.Channel, error) {
	r.openOnce.Do(func() {
		stream, err := r.client.Service().Open(ctx)
		if err != nil {
			r.openErr = fmt.Errorf("open UI stream: %w", err)
			return
		}
		r.channel = &channel{stream: stream, mutex: sync.Mutex{}}
	})
	return r.channel, r.openErr
}

// Close stops the selected UI process once.
func (r *Runtime) Close() {
	r.client.Close()
}

// Send writes one Host frame synchronously through the serialized stream path.
func (c *channel) Send(frame domainui.Frame) error {
	mapped, err := mapFrame(frame)
	if err != nil {
		return err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if sendErr := c.stream.Send(mapped); sendErr != nil {
		return fmt.Errorf("send UI frame: %w", sendErr)
	}
	return nil
}

// Receive blocks until the UI sends one command or the stream terminates.
func (c *channel) Receive() (domainui.Command, error) {
	command, err := c.stream.Recv()
	if err != nil {
		return domainui.Command{}, fmt.Errorf("receive UI command: %w", err)
	}
	return mapCommand(command)
}

// mapFrame converts one provider-neutral frame without exposing internal objects.
func mapFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	switch frame.Kind {
	case domainui.FrameInitialization:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Initialization{
			Initialization: mapInitialization(frame.Initialization),
		}}, nil
	case domainui.FrameLifecycle:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Lifecycle{
			Lifecycle: mapLifecycle(frame.Lifecycle),
		}}, nil
	case domainui.FrameAuthorization:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Authorization{
			Authorization: &uipb.AuthorizationRequest{Url: frame.AuthorizationURL},
		}}, nil
	case domainui.FrameInformation:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Information{
			Information: &uipb.Information{Text: frame.Text},
		}}, nil
	case domainui.FrameError:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Error{
			Error: &uipb.Error{Text: frame.Text, RetryAuthentication: frame.RetryAuthentication},
		}}, nil
	default:
		return nil, errors.New("map UI frame: payload is required")
	}
}

// mapInitialization converts one complete startup state.
func mapInitialization(initialization domainui.Initialization) *uipb.Initialization {
	startup := make([]*uipb.StartupContent, 0, len(initialization.StartupContent))
	for _, content := range initialization.StartupContent {
		startup = append(startup, &uipb.StartupContent{
			Severity: mapSeverity(content.Severity),
			Text:     content.Text,
		})
	}
	extensions := make([]*uipb.ExtensionAvailability, 0, len(initialization.Extensions))
	for _, extension := range initialization.Extensions {
		extensions = append(extensions, &uipb.ExtensionAvailability{
			PluginId: extension.PluginID,
			Tools:    append([]string(nil), extension.Tools...),
			Path:     extension.Path,
		})
	}
	return &uipb.Initialization{
		SelectedUiId:   initialization.SelectedUIID,
		StartupContent: startup,
		Extensions:     extensions,
		Availability:   mapAvailability(initialization.Availability),
	}
}

// mapLifecycle converts one explicit lifecycle payload.
func mapLifecycle(event domainui.Lifecycle) *uipb.LifecycleEvent {
	mapped := &uipb.LifecycleEvent{
		Type:            mapLifecycleType(event.Type),
		RunId:           event.RunID,
		Text:            event.Text,
		ToolCallId:      event.ToolCallID,
		ToolName:        event.ToolName,
		ProgressChannel: mapProgressChannel(event.ProgressChannel),
		IsError:         event.IsError,
		Outcome:         event.Outcome,
		ErrorMessage:    event.ErrorMessage,
		Availability:    mapAvailability(event.Availability),
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}
	if event.ModelContent.Type != 0 {
		mapped.ModelContent = &uipb.ModelContent{
			Type:     mapModelContentType(event.ModelContent.Type),
			Position: int32(event.ModelContent.Position), //nolint:gosec // Model positions remain bounded by response size.
			Text:     event.ModelContent.Text, Kind: mapModelContentKind(event.ModelContent.Kind),
		}
	}
	if event.Type == domainui.LifecycleMessageEnd {
		mapped.ModelResponse = mapModelResponse(event.ModelResponse)
	}
	if event.Type == domainui.LifecycleToolCallStart || event.Type == domainui.LifecycleToolCallDelta {
		mapped.ToolCallPreview = mapToolCallPreview(event.ToolCallPreview)
	}
	if event.Type == domainui.LifecycleToolCallEnd {
		arguments, _ := structpb.NewStruct(event.FinalToolCall.Arguments)
		mapped.FinalToolCall = &uipb.FinalToolCall{
			CallId: event.FinalToolCall.CallID, Name: event.FinalToolCall.Name,
			Position:  int32(event.FinalToolCall.Position), //nolint:gosec // Positions are bounded by response size.
			Arguments: arguments,
		}
	}
	return mapped
}

// mapCommand validates one generated UI command.
func mapCommand(command *uipb.OpenResponse) (domainui.Command, error) {
	switch {
	case command.GetSubmit() != nil:
		return domainui.Command{Kind: domainui.CommandSubmit, Text: command.GetSubmit().GetText()}, nil
	case command.GetStop() != nil:
		return domainui.Command{Kind: domainui.CommandStop, Text: ""}, nil
	case command.GetRetryAuthentication() != nil:
		return domainui.Command{Kind: domainui.CommandRetryAuthentication, Text: ""}, nil
	case command.GetQuit() != nil:
		return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
	default:
		return domainui.Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapSeverity converts startup severity to the public contract.
func mapSeverity(value domainui.ContentSeverity) uipb.ContentSeverity {
	switch value {
	case domainui.ContentSeverityInformation:
		return uipb.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	case domainui.ContentSeverityError:
		return uipb.ContentSeverity_CONTENT_SEVERITY_ERROR
	case domainui.ContentSeverityWarning:
		return uipb.ContentSeverity_CONTENT_SEVERITY_WARNING
	default:
		return uipb.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	}
}

// mapAvailability converts Host availability to the public contract.
func mapAvailability(value domainui.Availability) uipb.Availability {
	return uipb.Availability(value)
}

// mapLifecycleType converts Host lifecycle identity to the public contract.
//
//nolint:gocyclo // The flat switch maps the complete lifecycle enum.
func mapLifecycleType(value domainui.LifecycleType) uipb.LifecycleType {
	switch value {
	case domainui.LifecycleAgentStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_START
	case domainui.LifecycleTurnStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TURN_START
	case domainui.LifecycleMessageStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START
	case domainui.LifecycleModelContentStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START
	case domainui.LifecycleModelTextDelta:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA
	case domainui.LifecycleModelContentEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END
	case domainui.LifecycleToolCallStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START
	case domainui.LifecycleToolCallDelta:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA
	case domainui.LifecycleToolCallEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END
	case domainui.LifecycleMessageEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END
	case domainui.LifecycleToolExecutionStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START
	case domainui.LifecycleToolExecutionUpdate:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE
	case domainui.LifecycleToolExecutionEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END
	case domainui.LifecycleToolResult:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT
	case domainui.LifecycleTurnEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TURN_END
	case domainui.LifecycleAgentEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_END
	case domainui.LifecycleAgentSettled:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED
	case domainui.LifecycleAvailabilityChanged:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED
	default:
		return uipb.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED
	}
}

func mapToolCallPreview(preview domainui.ToolCallPreview) *uipb.ToolCallPreview {
	fields := make([]*uipb.ToolCallPreviewField, len(preview.Fields))
	for index, field := range preview.Fields {
		mapped := &uipb.ToolCallPreviewField{Name: field.Name, Content: nil}
		if field.Complete {
			value, _ := structpb.NewValue(field.Value)
			mapped.Content = &uipb.ToolCallPreviewField_Value{Value: value}
		} else {
			mapped.Content = &uipb.ToolCallPreviewField_Prefix{Prefix: field.Prefix}
		}
		fields[index] = mapped
	}
	return &uipb.ToolCallPreview{
		CallId: preview.CallID, Name: preview.Name,
		Position:    int32(preview.Position), //nolint:gosec // Positions are bounded by response size.
		Provisional: preview.Provisional, Fields: fields,
	}
}

func mapModelResponse(response domainui.ModelResponse) *uipb.ModelResponse {
	content := make([]*uipb.ModelResponseContent, len(response.Content))
	for index, item := range response.Content {
		content[index] = &uipb.ModelResponseContent{Kind: mapModelContentKind(item.Kind), Text: item.Text}
	}
	diagnostics := make([]*uipb.ModelDiagnostic, len(response.Diagnostics))
	for index, diagnostic := range response.Diagnostics {
		diagnostics[index] = &uipb.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	}
	return &uipb.ModelResponse{
		Text: response.Text, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: response.ResponseModel,
		ResponseId: response.ResponseID,
		Usage: &uipb.ModelUsage{
			InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
			CachedInputTokens: response.Usage.CachedInputTokens, CacheWriteTokens: response.Usage.CacheWriteTokens,
			ReasoningTokens: response.Usage.ReasoningTokens, TotalTokens: response.Usage.TotalTokens,
		},
		Diagnostics: diagnostics, Content: content,
	}
}

func mapModelContentKind(value domainui.ModelContentKind) uipb.ModelContentKind {
	switch value {
	case domainui.ModelContentKindText:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_TEXT
	case domainui.ModelContentKindRefusal:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL
	case domainui.ModelContentKindReasoning:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_REASONING
	default:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED
	}
}

func mapModelContentType(value domainui.ModelContentType) uipb.ModelContentType {
	switch value {
	case domainui.ModelContentStart:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_START
	case domainui.ModelContentTextDelta:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA
	case domainui.ModelContentEnd:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_END
	default:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED
	}
}

// mapProgressChannel converts Host tool progress identity to the public contract.
func mapProgressChannel(value domainui.ProgressChannel) uipb.ProgressChannel {
	return uipb.ProgressChannel(value)
}
