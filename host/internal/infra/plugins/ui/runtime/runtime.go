// Package runtime adapts the public UI SDK to Host UI lifecycle contracts.
//
//nolint:exhaustruct // Protobuf oneof builders intentionally set only the active field.
package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"sync"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/structpb"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
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
		request := &uipb.OpenRequest{}
		request.SetInitialization(mapInitialization(frame.Initialization))
		return request, nil
	case domainui.FrameLifecycle:
		request := &uipb.OpenRequest{}
		request.SetLifecycle(mapLifecycle(frame.Lifecycle))
		return request, nil
	case domainui.FrameAuthorization:
		request := &uipb.OpenRequest{}
		request.SetAuthorization(uipb.AuthorizationRequest_builder{Url: new(frame.AuthorizationURL)}.Build())
		return request, nil
	case domainui.FrameInformation:
		request := &uipb.OpenRequest{}
		request.SetInformation(uipb.Information_builder{Text: new(frame.Text)}.Build())
		return request, nil
	case domainui.FrameError:
		request := &uipb.OpenRequest{}
		request.SetError(uipb.Error_builder{
			Text: new(frame.Text), RetryAuthentication: new(frame.RetryAuthentication),
		}.Build())
		return request, nil
	case domainui.FrameModelSelectionChanged:
		request := &uipb.OpenRequest{}
		request.SetModelSelectionChanged(uipb.ModelSelectionChanged_builder{
			Selection: mapModelSelection(frame.ModelSelection),
		}.Build())
		return request, nil
	default:
		return nil, errors.New("map UI frame: payload is required")
	}
}

// mapInitialization converts one complete startup state.
func mapInitialization(initialization domainui.Initialization) *uipb.Initialization {
	startup := lo.Map(initialization.StartupContent, func(content domainui.StartupContent, _ int) *uipb.StartupContent {
		return uipb.StartupContent_builder{
			Severity: new(mapSeverity(content.Severity)),
			Text:     new(content.Text),
		}.Build()
	})
	extensions := lo.Map(
		initialization.Extensions,
		func(extension domainui.ExtensionAvailability, _ int) *uipb.ExtensionAvailability {
			return uipb.ExtensionAvailability_builder{
				PluginId: new(extension.PluginID),
				Tools:    slices.Clone(extension.Tools),
				Path:     new(extension.Path),
			}.Build()
		},
	)
	models := lo.Map(initialization.Models, func(configured domainui.ConfiguredModel, _ int) *uipb.ConfiguredModel {
		choices := lo.Map(configured.Reasoning.Choices, func(choice domainui.ReasoningChoice, _ int) uipb.ReasoningChoice {
			return mapReasoningChoice(choice)
		})
		reasoning := uipb.ReasoningCapabilities_builder{
			Supported: new(configured.Reasoning.Supported), Choices: choices,
			DefaultChoice: new(mapReasoningChoice(configured.Reasoning.Default)),
		}.Build()
		return uipb.ConfiguredModel_builder{
			ProviderId: new(configured.ProviderID), ModelId: new(configured.ModelID), Reasoning: reasoning,
		}.Build()
	})
	return uipb.Initialization_builder{
		SelectedUiId:   new(initialization.SelectedUIID),
		StartupContent: startup,
		Extensions:     extensions,
		Availability:   new(mapAvailability(initialization.Availability)),
		Models:         models,
		ModelSelection: mapModelSelection(initialization.ModelSelection),
	}.Build()
}

// mapModelSelection converts one Host-confirmed selection.
func mapModelSelection(selection domainui.ModelSelection) *uipb.ModelSelection {
	return uipb.ModelSelection_builder{
		ProviderId: new(selection.ProviderID), ModelId: new(selection.ModelID),
		ReasoningChoice: new(mapReasoningChoice(selection.ReasoningChoice)),
	}.Build()
}

// mapLifecycle converts one explicit lifecycle payload.
func mapLifecycle(event domainui.Lifecycle) *uipb.LifecycleEvent {
	mapped := uipb.LifecycleEvent_builder{
		Type:            new(mapLifecycleType(event.Type)),
		RunId:           new(event.RunID),
		Text:            new(event.Text),
		ToolCallId:      new(event.ToolCallID),
		ToolName:        new(event.ToolName),
		ProgressChannel: new(mapProgressChannel(event.ProgressChannel)),
		IsError:         new(event.IsError),
		Outcome:         new(event.Outcome),
		ErrorMessage:    new(event.ErrorMessage),
		Availability:    new(mapAvailability(event.Availability)),
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build()
	if event.ModelContent.Type != 0 {
		mapped.SetModelContent(uipb.ModelContent_builder{
			Type:     new(mapModelContentType(event.ModelContent.Type)),
			Position: new(int32(event.ModelContent.Position)), //nolint:gosec // Model positions remain bounded by response size.
			Text:     new(event.ModelContent.Text), Kind: new(mapModelContentKind(event.ModelContent.Kind)),
		}.Build())
	}
	if event.Type == domainui.LifecycleMessageEnd {
		mapped.SetModelResponse(mapModelResponse(event.ModelResponse))
	}
	if event.Type == domainui.LifecycleToolCallStart || event.Type == domainui.LifecycleToolCallDelta {
		mapped.SetToolCallPreview(mapToolCallPreview(event.ToolCallPreview))
	}
	if event.Type == domainui.LifecycleToolResult {
		mapped.SetToolResultContents(mapToolResultContents(event.ToolResultContents))
	}
	if event.Type == domainui.LifecycleToolCallEnd {
		arguments, _ := structpb.NewStruct(event.FinalToolCall.Arguments)
		mapped.SetFinalToolCall(uipb.FinalToolCall_builder{
			CallId: new(event.FinalToolCall.CallID), Name: new(event.FinalToolCall.Name),
			Position:  new(int32(event.FinalToolCall.Position)), //nolint:gosec // Positions are bounded by response size.
			Arguments: arguments,
		}.Build())
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
	case command.GetSelectModel() != nil:
		selected := command.GetSelectModel()
		if selected.GetProviderId() == "" || selected.GetModelId() == "" {
			return domainui.Command{}, errors.New("receive UI command: provider and model are required")
		}
		return domainui.Command{
			Kind: domainui.CommandSelectModel, ProviderID: selected.GetProviderId(), ModelID: selected.GetModelId(),
		}, nil
	case command.GetSelectReasoningChoice() != nil:
		level, err := mapReasoningChoiceFromProto(command.GetSelectReasoningChoice().GetChoice())
		if err != nil {
			return domainui.Command{}, err
		}
		return domainui.Command{Kind: domainui.CommandSelectReasoningChoice, ReasoningChoice: level}, nil
	default:
		return domainui.Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapReasoningChoice converts a Host reasoning choice to the public contract.
func mapReasoningChoice(value domainui.ReasoningChoice) uipb.ReasoningChoice {
	switch value {
	case domainui.ReasoningChoiceOff:
		return uipb.ReasoningChoice_REASONING_CHOICE_OFF
	case domainui.ReasoningChoiceOn:
		return uipb.ReasoningChoice_REASONING_CHOICE_ON
	case domainui.ReasoningChoiceMinimal:
		return uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL
	case domainui.ReasoningChoiceLow:
		return uipb.ReasoningChoice_REASONING_CHOICE_LOW
	case domainui.ReasoningChoiceMedium:
		return uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM
	case domainui.ReasoningChoiceHigh:
		return uipb.ReasoningChoice_REASONING_CHOICE_HIGH
	case domainui.ReasoningChoiceXHigh:
		return uipb.ReasoningChoice_REASONING_CHOICE_XHIGH
	case domainui.ReasoningChoiceMax:
		return uipb.ReasoningChoice_REASONING_CHOICE_MAX
	default:
		return uipb.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	}
}

// mapReasoningChoiceFromProto rejects unspecified and unknown public values.
func mapReasoningChoiceFromProto(value uipb.ReasoningChoice) (domainui.ReasoningChoice, error) {
	switch value {
	case uipb.ReasoningChoice_REASONING_CHOICE_OFF:
		return domainui.ReasoningChoiceOff, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_ON:
		return domainui.ReasoningChoiceOn, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return domainui.ReasoningChoiceMinimal, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_LOW:
		return domainui.ReasoningChoiceLow, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return domainui.ReasoningChoiceMedium, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_HIGH:
		return domainui.ReasoningChoiceHigh, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return domainui.ReasoningChoiceXHigh, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_MAX:
		return domainui.ReasoningChoiceMax, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return 0, errors.New("receive UI command: reasoning choice is unspecified")
	default:
		return 0, fmt.Errorf("receive UI command: unknown reasoning choice %d", value)
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

// mapToolResultContents copies ordered domain blocks into the public UI contract.
func mapToolResultContents(contents []tool.ResultContent) []*uipb.ToolResultContent {
	return lo.FilterMap(contents, func(content tool.ResultContent, _ int) (*uipb.ToolResultContent, bool) {
		switch content.Kind {
		case tool.ResultContentText:
			text := content.Text.OrEmpty()
			return uipb.ToolResultContent_builder{Text: &text}.Build(), true
		case tool.ResultContentImage:
			image := content.Image.OrEmpty()
			return uipb.ToolResultContent_builder{Image: uipb.ToolResultImage_builder{
				MediaType: &image.MediaType, Data: bytes.Clone(image.Data),
			}.Build()}.Build(), true
		}
		return nil, false
	})
}

func mapToolCallPreview(preview domainui.ToolCallPreview) *uipb.ToolCallPreview {
	fields := lo.Map(preview.Fields, func(field domainui.ToolCallPreviewField, _ int) *uipb.ToolCallPreviewField {
		mapped := uipb.ToolCallPreviewField_builder{Name: new(field.Name)}.Build()
		if field.Complete {
			value, _ := structpb.NewValue(field.Value)
			mapped.SetValue(proto.ValueOrDefault(value))
		} else {
			mapped.SetPrefix(field.Prefix)
		}
		return mapped
	})
	return uipb.ToolCallPreview_builder{
		CallId: new(preview.CallID), Name: new(preview.Name),
		Position:    new(int32(preview.Position)), //nolint:gosec // Positions are bounded by response size.
		Provisional: new(preview.Provisional), Fields: fields,
	}.Build()
}

func mapModelResponse(response domainui.ModelResponse) *uipb.ModelResponse {
	content := lo.Map(response.Content, func(item domainui.ModelResponseContent, _ int) *uipb.ModelResponseContent {
		return uipb.ModelResponseContent_builder{
			Kind: new(mapModelContentKind(item.Kind)), Text: new(item.Text),
		}.Build()
	})
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic domainui.ModelDiagnostic, _ int) *uipb.ModelDiagnostic {
		return uipb.ModelDiagnostic_builder{
			Code: new(diagnostic.Code), Message: new(diagnostic.Message),
		}.Build()
	})
	return uipb.ModelResponse_builder{
		Text: new(response.Text), Outcome: new(response.Outcome), ErrorMessage: new(response.ErrorMessage),
		Provider: new(response.Provider), Model: new(response.Model), ResponseModel: response.ResponseModel,
		ResponseId: new(response.ResponseID),
		Usage: uipb.ModelUsage_builder{
			InputTokens: new(response.Usage.InputTokens), OutputTokens: new(response.Usage.OutputTokens),
			CachedInputTokens: new(response.Usage.CachedInputTokens), CacheWriteTokens: new(response.Usage.CacheWriteTokens),
			ReasoningTokens: new(response.Usage.ReasoningTokens), TotalTokens: new(response.Usage.TotalTokens),
		}.Build(),
		Diagnostics: diagnostics, Content: content,
	}.Build()
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
