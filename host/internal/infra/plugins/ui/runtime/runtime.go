// Package runtime adapts the public UI SDK to Host UI lifecycle contracts.
package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
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
	cancel context.CancelFunc
	closed atomic.Bool
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
	return &Runtime{
		client:   client,
		openOnce: sync.Once{},
		channel:  nil,
		openErr:  nil,
	}, nil
}

// Capabilities returns the immutable capabilities retrieved before stream creation.
func (r *Runtime) Capabilities() domainui.Capabilities {
	return domainui.Capabilities{
		ControlsTerminal: r.client.Capabilities().GetControlsTerminal(),
	}
}

// Open opens and reuses the one persistent UI lifecycle stream.
func (r *Runtime) Open(ctx context.Context) (hostui.Channel, error) {
	r.openOnce.Do(func() {
		streamContext, cancel := context.WithCancel(ctx)
		stream, err := r.client.Service().Open(streamContext)
		if err != nil {
			cancel()
			r.openErr = fmt.Errorf("open UI stream: %w", err)
			return
		}
		r.channel = &channel{
			stream: stream,
			cancel: cancel,
			closed: atomic.Bool{},
			mutex:  sync.Mutex{},
		}
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
		streamErr := c.stream.Context().Err()
		if errors.Is(sendErr, context.Canceled) || status.Code(sendErr) == codes.Canceled ||
			errors.Is(sendErr, io.EOF) && errors.Is(streamErr, context.Canceled) {
			return fmt.Errorf("send UI frame: %w", context.Canceled)
		}
		wrappedSendErr := fmt.Errorf("send UI frame: %w", sendErr)
		if streamErr != nil &&
			!errors.Is(sendErr, streamErr) && !errors.Is(streamErr, sendErr) {
			return errors.Join(wrappedSendErr, fmt.Errorf("UI stream context: %w", streamErr))
		}
		return wrappedSendErr
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

// Close cancels the stream context to unblock pending send and receive calls.
func (c *channel) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.cancel()
	}
}

// mapFrame converts one provider-neutral frame without exposing internal objects.
func mapFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	if request, handled, err := mapSessionFrame(frame); handled {
		return request, err
	}
	switch frame.Kind {
	case domainui.FrameInitialization:
		return mapInitializationFrame(frame)
	case domainui.FrameLifecycle:
		return mapLifecycleFrame(frame)
	case domainui.FrameAuthorization:
		authorizationURL, present := frame.AuthorizationURL.Get()
		if !present {
			return nil, errors.New("map UI frame: authorization payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetAuthorization(uipb.AuthorizationRequest_builder{
			Url: new(authorizationURL),
		}.Build())
		return request, nil
	case domainui.FrameInformation:
		text, present := frame.Text.Get()
		if !present {
			return nil, errors.New("map UI frame: information payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetInformation(uipb.Information_builder{
			Text: new(text),
		}.Build())
		return request, nil
	case domainui.FrameError:
		text, hasText := frame.Text.Get()
		retryAuthentication, hasRetryAuthentication := frame.RetryAuthentication.Get()
		if !hasText || !hasRetryAuthentication {
			return nil, errors.New("map UI frame: error payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetError(uipb.Error_builder{
			Text:                new(text),
			RetryAuthentication: new(retryAuthentication),
		}.Build())
		return request, nil
	case domainui.FrameModelSelectionChanged:
		selection, present := frame.ModelSelection.Get()
		if !present {
			return nil, errors.New("map UI frame: model selection payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetModelSelectionChanged(uipb.ModelSelectionChanged_builder{
			Selection: mapModelSelection(selection),
		}.Build())
		return request, nil
	case domainui.FrameSessionList, domainui.FrameSessionChanged, domainui.FrameSessionInformation:
		return nil, errors.New("map UI frame: session frame was not mapped")
	default:
		return nil, errors.New("map UI frame: payload is required")
	}
}

// mapSessionFrame maps lifecycle frames to protobuf payloads without losing optional fields.
func mapSessionFrame(frame domainui.Frame) (*uipb.OpenRequest, bool, error) {
	request := new(uipb.OpenRequest)
	switch frame.Kind {
	case domainui.FrameSessionList:
		mapped := lo.Map(frame.Sessions, func(value session.Summary, _ int) *uipb.SessionSummary {
			return mapSessionSummary(value)
		})
		request.SetSessionList(uipb.SessionList_builder{Sessions: mapped}.Build())
		return request, true, nil
	case domainui.FrameSessionChanged, domainui.FrameSessionInformation:
		info, present := frame.SessionInfo.Get()
		if !present {
			return nil, true, errors.New("map UI frame: session information is required")
		}
		if frame.Kind == domainui.FrameSessionChanged {
			entries, err := mapRestoredSessionEntries(frame.SessionEntries)
			if err != nil {
				return nil, true, err
			}
			request.SetSessionChanged(uipb.SessionChanged_builder{
				Info: mapSessionInfo(info), Entries: entries,
			}.Build())
		} else {
			statistics, statisticsPresent := frame.SessionStatistics.Get()
			if !statisticsPresent {
				return nil, true, errors.New("map UI frame: session statistics are required")
			}
			request.SetSessionInformation(uipb.SessionInformation_builder{
				Info: mapSessionInfo(info), Statistics: mapSessionStatistics(statistics),
			}.Build())
		}
		return request, true, nil
	case domainui.FrameInitialization, domainui.FrameLifecycle, domainui.FrameAuthorization,
		domainui.FrameInformation, domainui.FrameError, domainui.FrameModelSelectionChanged:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

func mapRestoredSessionEntries(entries []domainui.SessionEntry) ([]*uipb.SessionEntry, error) {
	return lo.MapErr(entries, func(entry domainui.SessionEntry, index int) (*uipb.SessionEntry, error) {
		wire := new(uipb.SessionEntry)
		wire.SetId(entry.ID)
		wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
		switch entry.Kind {
		case domainui.SessionEntryUser:
			user, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("map restored session entry %d: user payload is missing", index)
			}
			content, err := lo.MapErr(user.Content, func(item model.InputContent, contentIndex int) (*uipb.UserContent, error) {
				wireContent := new(uipb.UserContent)
				switch item.Kind {
				case model.InputContentText:
					text, hasText := item.Text.Get()
					if !hasText || item.MediaType.IsSome() || item.Data.IsSome() {
						return nil, fmt.Errorf("map restored user content %d: invalid text payload", contentIndex)
					}
					wireContent.SetText(text)
				case model.InputContentImage:
					mediaType, hasMediaType := item.MediaType.Get()
					data, hasData := item.Data.Get()
					if item.Text.IsSome() || !hasMediaType || !hasData {
						return nil, fmt.Errorf("map restored user content %d: invalid image payload", contentIndex)
					}
					image := uipb.UserImage_builder{
						MediaType: new(mediaType), Data: nil,
					}.Build()
					image.SetData(bytes.Clone(data))
					wireContent.SetImage(image)
				default:
					return nil, fmt.Errorf("map restored user content %d: unknown kind %d", contentIndex, item.Kind)
				}
				return wireContent, nil
			})
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetUser(uipb.UserMessage_builder{Content: content}.Build())
		case domainui.SessionEntryModel:
			response, _ := entry.Model.Get()
			mapped, err := mapModelResponse(response)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetModel(mapped)
		case domainui.SessionEntryToolResult:
			result, _ := entry.ToolResult.Get()
			wire.SetToolResult(uipb.ToolResult_builder{
				CallId: new(result.CallID), ToolName: new(result.ToolName),
				Contents: mapToolResultContents(result.Contents), IsError: new(result.IsError),
			}.Build())
		}
		return wire, nil
	})
}

// mapInitializationFrame validates and maps the selected initialization payload.
func mapInitializationFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	initialization, present := frame.Initialization.Get()
	if !present {
		return nil, errors.New("map UI frame: initialization payload is required")
	}
	mapped, err := mapInitialization(initialization)
	if err != nil {
		return nil, err
	}
	request := &uipb.OpenRequest{}
	request.SetInitialization(mapped)
	return request, nil
}

// mapLifecycleFrame validates and maps the selected lifecycle payload.
func mapLifecycleFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	lifecycle, present := frame.Lifecycle.Get()
	if !present {
		return nil, errors.New("map UI frame: lifecycle payload is required")
	}
	mapped, err := mapLifecycle(lifecycle)
	if err != nil {
		return nil, err
	}
	request := &uipb.OpenRequest{}
	request.SetLifecycle(mapped)
	return request, nil
}

// mapInitialization converts one complete startup state.
func mapInitialization(initialization domainui.Initialization) (*uipb.Initialization, error) {
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
			Supported:     new(configured.Reasoning.Supported),
			Choices:       choices,
			DefaultChoice: new(mapReasoningChoice(configured.Reasoning.Default)),
		}.Build()
		return uipb.ConfiguredModel_builder{
			ProviderId: new(configured.ProviderID),
			ModelId:    new(configured.ModelID),
			Reasoning:  reasoning,
		}.Build()
	})
	selection, present := initialization.ModelSelection.Get()
	if !present {
		return nil, errors.New("map UI initialization: model selection is required")
	}
	return uipb.Initialization_builder{
		SelectedUiId:   new(initialization.SelectedUIID),
		StartupContent: startup,
		Extensions:     extensions,
		Availability:   new(mapAvailability(initialization.Availability)),
		Models:         models,
		ModelSelection: mapModelSelection(selection),
		SessionInfo:    mapSessionInfo(initialization.SessionInfo),
	}.Build(), nil
}

// mapModelSelection converts one Host-confirmed selection.
func mapModelSelection(selection domainui.ModelSelection) *uipb.ModelSelection {
	return uipb.ModelSelection_builder{
		ProviderId:      new(selection.ProviderID),
		ModelId:         new(selection.ModelID),
		ReasoningChoice: new(mapReasoningChoice(selection.ReasoningChoice)),
	}.Build()
}

// mapLifecycle converts one explicit lifecycle payload.
func mapLifecycle(event domainui.Lifecycle) (*uipb.LifecycleEvent, error) {
	mapped := mapLifecycleScalars(event)
	if event.Type != domainui.LifecycleAvailabilityChanged && event.RunID.IsNone() {
		return nil, errors.New("map UI lifecycle: run ID is required")
	}
	switch event.Type {
	case domainui.LifecycleAgentStart, domainui.LifecycleTurnStart, domainui.LifecycleMessageStart,
		domainui.LifecycleAgentSettled:
		return mapped, nil
	case domainui.LifecycleModelContentStart, domainui.LifecycleModelTextDelta,
		domainui.LifecycleModelContentEnd, domainui.LifecycleMessageEnd:
		return mapModelLifecycle(event, mapped)
	case domainui.LifecycleToolCallStart, domainui.LifecycleToolCallDelta, domainui.LifecycleToolCallEnd:
		return mapToolCallLifecycle(event, mapped)
	case domainui.LifecycleToolExecutionStart, domainui.LifecycleToolExecutionUpdate,
		domainui.LifecycleToolExecutionEnd, domainui.LifecycleToolResult:
		return mapToolExecutionLifecycle(event, mapped)
	case domainui.LifecycleTurnEnd, domainui.LifecycleAgentEnd:
		return mapTerminalLifecycle(event, mapped)
	case domainui.LifecycleAvailabilityChanged:
		if event.Availability.IsNone() {
			return nil, errors.New("map UI lifecycle: availability is required")
		}
		return mapped, nil
	}
	return mapped, nil
}

// mapLifecycleScalars maps scalar Options at the generated Protobuf boundary.
func mapLifecycleScalars(event domainui.Lifecycle) *uipb.LifecycleEvent {
	var runID *string
	if value, present := event.RunID.Get(); present {
		runID = new(value)
	}
	var text *string
	if value, present := event.Text.Get(); present {
		text = new(value)
	}
	var toolCallID *string
	if value, present := event.ToolCallID.Get(); present {
		toolCallID = new(value)
	}
	var toolName *string
	if value, present := event.ToolName.Get(); present {
		toolName = new(value)
	}
	var mappedProgressChannel *uipb.ProgressChannel
	if value, present := event.ProgressChannel.Get(); present {
		mappedProgressChannel = new(mapProgressChannel(value))
	}
	var isError *bool
	if value, present := event.IsError.Get(); present {
		isError = new(value)
	}
	var outcome *string
	if value, present := event.Outcome.Get(); present {
		outcome = new(value)
	}
	var errorMessage *string
	if value, present := event.ErrorMessage.Get(); present {
		errorMessage = new(value)
	}
	var availability *uipb.Availability
	if value, present := event.Availability.Get(); present {
		availability = new(mapAvailability(value))
	}
	return uipb.LifecycleEvent_builder{
		Type:               new(mapLifecycleType(event.Type)),
		RunId:              runID,
		Text:               text,
		ToolCallId:         toolCallID,
		ToolName:           toolName,
		ProgressChannel:    mappedProgressChannel,
		IsError:            isError,
		Outcome:            outcome,
		ErrorMessage:       errorMessage,
		Availability:       availability,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build()
}

// mapModelLifecycle validates and maps selected model payloads.
func mapModelLifecycle(event domainui.Lifecycle, mapped *uipb.LifecycleEvent) (*uipb.LifecycleEvent, error) {
	if event.Type == domainui.LifecycleMessageEnd {
		response, present := event.ModelResponse.Get()
		if !present {
			return nil, errors.New("map UI lifecycle: model response is required")
		}
		mappedResponse, err := mapModelResponse(response)
		if err != nil {
			return nil, err
		}
		mapped.SetModelResponse(mappedResponse)
		return mapped, nil
	}
	content, present := event.ModelContent.Get()
	if !present {
		return nil, errors.New("map UI lifecycle: model content is required")
	}
	var contentText *string
	if value, hasText := content.Text.Get(); hasText {
		contentText = new(value)
	}
	if event.Type == domainui.LifecycleModelTextDelta && contentText == nil {
		return nil, errors.New("map UI lifecycle: model text delta is required")
	}
	mapped.SetModelContent(uipb.ModelContent_builder{
		Type:     new(mapModelContentType(content.Type)),
		Position: new(int32(content.Position)), //nolint:gosec // Model positions remain bounded by response size.
		Text:     contentText,
		Kind:     new(mapModelContentKind(content.Kind)),
	}.Build())
	return mapped, nil
}

// mapToolCallLifecycle validates and maps selected tool-call payloads.
func mapToolCallLifecycle(event domainui.Lifecycle, mapped *uipb.LifecycleEvent) (*uipb.LifecycleEvent, error) {
	if event.Type == domainui.LifecycleToolCallEnd {
		call, present := event.FinalToolCall.Get()
		if !present {
			return nil, errors.New("map UI lifecycle: final tool call is required")
		}
		arguments, err := structpb.NewStruct(call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("map UI lifecycle final tool call: %w", err)
		}
		mapped.SetFinalToolCall(uipb.FinalToolCall_builder{
			CallId:    new(call.CallID),
			Name:      new(call.Name),
			Position:  new(int32(call.Position)), //nolint:gosec // Positions are bounded by response size.
			Arguments: arguments,
		}.Build())
		return mapped, nil
	}
	preview, present := event.ToolCallPreview.Get()
	if !present {
		return nil, errors.New("map UI lifecycle: tool call preview is required")
	}
	mappedPreview, err := mapToolCallPreview(preview)
	if err != nil {
		return nil, err
	}
	mapped.SetToolCallPreview(mappedPreview)
	return mapped, nil
}

// mapToolExecutionLifecycle validates and maps selected tool-execution payloads.
func mapToolExecutionLifecycle(event domainui.Lifecycle, mapped *uipb.LifecycleEvent) (*uipb.LifecycleEvent, error) {
	switch event.Type {
	case domainui.LifecycleToolExecutionStart:
		if event.ToolCallID.IsNone() || event.ToolName.IsNone() {
			return nil, errors.New("map UI lifecycle: tool execution is required")
		}
	case domainui.LifecycleToolExecutionUpdate:
		if event.Text.IsNone() || event.ProgressChannel.IsNone() {
			return nil, errors.New("map UI lifecycle: tool progress is required")
		}
	case domainui.LifecycleToolExecutionEnd, domainui.LifecycleToolResult:
		contents, hasContents := event.ToolResultContents.Get()
		if event.ToolCallID.IsNone() || event.ToolName.IsNone() || !hasContents || event.IsError.IsNone() {
			return nil, errors.New("map UI lifecycle: tool result is required")
		}
		if event.Type == domainui.LifecycleToolResult {
			mapped.SetToolResultContents(mapToolResultContents(contents))
		}
	case domainui.LifecycleAgentStart, domainui.LifecycleTurnStart, domainui.LifecycleMessageStart,
		domainui.LifecycleModelContentStart, domainui.LifecycleModelTextDelta,
		domainui.LifecycleModelContentEnd, domainui.LifecycleToolCallStart,
		domainui.LifecycleToolCallDelta, domainui.LifecycleToolCallEnd, domainui.LifecycleMessageEnd,
		domainui.LifecycleTurnEnd, domainui.LifecycleAgentEnd,
		domainui.LifecycleAgentSettled, domainui.LifecycleAvailabilityChanged:
		return nil, fmt.Errorf("map UI lifecycle: unsupported tool execution event type %d", event.Type)
	}
	return mapped, nil
}

// mapTerminalLifecycle validates selected turn and agent summaries.
func mapTerminalLifecycle(event domainui.Lifecycle, mapped *uipb.LifecycleEvent) (*uipb.LifecycleEvent, error) {
	switch event.Type {
	case domainui.LifecycleTurnEnd:
		if event.Text.IsNone() {
			return nil, errors.New("map UI lifecycle: turn summary is required")
		}
	case domainui.LifecycleAgentEnd:
		if event.Outcome.IsNone() {
			return nil, errors.New("map UI lifecycle: agent summary is required")
		}
	case domainui.LifecycleAgentStart, domainui.LifecycleTurnStart, domainui.LifecycleMessageStart,
		domainui.LifecycleModelContentStart, domainui.LifecycleModelTextDelta,
		domainui.LifecycleModelContentEnd, domainui.LifecycleToolCallStart,
		domainui.LifecycleToolCallDelta, domainui.LifecycleToolCallEnd, domainui.LifecycleMessageEnd,
		domainui.LifecycleToolExecutionStart, domainui.LifecycleToolExecutionUpdate,
		domainui.LifecycleToolExecutionEnd, domainui.LifecycleToolResult,
		domainui.LifecycleAgentSettled, domainui.LifecycleAvailabilityChanged:
		return nil, fmt.Errorf("map UI lifecycle: unsupported terminal event type %d", event.Type)
	}
	return mapped, nil
}

// mapSessionInfo preserves absent name and storage path through protobuf scalar presence.
func mapSessionInfo(info session.Info) *uipb.SessionInfo {
	wire := uipb.SessionInfo_builder{
		Id:               new(string(info.ID)),
		WorkingDirectory: new(info.WorkingDirectory),
		CreatedTime:      timestamppb.New(info.CreatedAt),
		UpdateTime:       timestamppb.New(info.UpdatedAt), Name: nil, StoragePath: nil,
	}.Build()
	if name, present := info.Name.Get(); present {
		wire.SetName(name)
	}
	if path, present := info.StoragePath.Get(); present {
		wire.SetStoragePath(path)
	}
	return wire
}

// mapSessionStatistics preserves counts and optional complete token and cost values on the UI wire boundary.
func mapSessionStatistics(statistics session.Statistics) *uipb.SessionStatistics {
	wire := uipb.SessionStatistics_builder{
		UserMessages: new(int64(statistics.UserMessages)), ModelResponses: new(int64(statistics.ModelResponses)),
		ToolCalls: new(int64(statistics.ToolCalls)), ToolResults: new(int64(statistics.ToolResults)),
		TotalMessages: new(int64(statistics.TotalMessages)), Tokens: nil,
		EstimatedCost: nil, CostBreakdown: nil,
	}.Build()
	if usage, present := statistics.TokenUsage.Get(); present {
		wire.SetTokens(uipb.TokenUsage_builder{
			InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
			CacheReadTokens: new(usage.CacheReadTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
			ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
		}.Build())
	}
	if cost, present := statistics.EstimatedCost.Get(); present {
		wire.SetEstimatedCost(mapEstimatedCost(cost))
	}
	breakdown := make([]*uipb.ProviderModelCost, len(statistics.CostBreakdown))
	for groupIndex := range statistics.CostBreakdown {
		group := &statistics.CostBreakdown[groupIndex]
		mapped := uipb.ProviderModelCost_builder{
			ProviderId: new(string(group.Provider)), ModelId: new(string(group.Model)), EstimatedCost: nil,
		}.Build()
		if cost, present := group.EstimatedCost.Get(); present {
			mapped.SetEstimatedCost(mapEstimatedCost(cost))
		}
		breakdown[groupIndex] = mapped
	}
	wire.SetCostBreakdown(breakdown)
	return wire
}

// mapEstimatedCost preserves calculated cost buckets and their stored total.
func mapEstimatedCost(cost session.EstimatedCost) *uipb.EstimatedCost {
	return uipb.EstimatedCost_builder{
		Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
		CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
	}.Build()
}

// mapSessionSummary preserves optional row labels while mapping an ordered list entry.
func mapSessionSummary(summary session.Summary) *uipb.SessionSummary {
	wire := uipb.SessionSummary_builder{
		Info:          mapSessionInfo(summary.Info),
		TotalMessages: new(int64(summary.TotalMessages)), FirstUserText: nil,
	}.Build()
	if text, present := summary.FirstUserText.Get(); present {
		wire.SetFirstUserText(text)
	}
	return wire
}

// mapCommand validates one generated UI command.
func mapCommand(command *uipb.OpenResponse) (domainui.Command, error) {
	if mapped, handled, err := mapSessionCommand(command); handled {
		return mapped, err
	}
	if mapped, handled, err := mapSelectionCommand(command); handled {
		return mapped, err
	}
	switch {
	case command.GetSubmit() != nil:
		submit := command.GetSubmit()
		if !submit.HasText() {
			return domainui.Command{}, errors.New("receive UI command: submit text is required")
		}
		return domainui.Command{
			Kind:            domainui.CommandSubmit,
			Text:            mo.Some(submit.GetText()),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetStop() != nil:
		return domainui.Command{
			Kind:            domainui.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetRetryAuthentication() != nil:
		return domainui.Command{
			Kind:            domainui.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetQuit() != nil:
		return domainui.Command{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetSelectModel() != nil, command.GetSelectReasoningChoice() != nil:
		return domainui.Command{}, errors.New("receive UI command: selection command was not mapped")
	case command.GetCreateSession() != nil, command.GetListSessions() != nil,
		command.GetGetSessionInfo() != nil, command.GetResumeSession() != nil,
		command.GetSetSessionName() != nil:
		return domainui.Command{}, errors.New("receive UI command: session command was not mapped")
	default:
		return domainui.Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapSelectionCommand validates model and reasoning payloads before they reach the Host use case.
func mapSelectionCommand(command *uipb.OpenResponse) (domainui.Command, bool, error) {
	switch {
	case command.GetSelectModel() != nil:
		selected := command.GetSelectModel()
		if !selected.HasProviderId() || !selected.HasModelId() ||
			selected.GetProviderId() == "" || selected.GetModelId() == "" {
			return domainui.Command{}, true, errors.New("receive UI command: provider and model are required")
		}
		return domainui.Command{
			Kind:            domainui.CommandSelectModel,
			ProviderID:      mo.Some(selected.GetProviderId()),
			ModelID:         mo.Some(selected.GetModelId()),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, true, nil
	case command.GetSelectReasoningChoice() != nil:
		selected := command.GetSelectReasoningChoice()
		if !selected.HasChoice() {
			return domainui.Command{}, true, errors.New("receive UI command: reasoning choice is required")
		}
		level, err := mapReasoningChoiceFromProto(selected.GetChoice())
		if err != nil {
			return domainui.Command{}, true, err
		}
		return domainui.Command{
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(level),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, true, nil
	default:
		return domainui.Command{}, false, nil
	}
}

// mapSessionCommand validates lifecycle command arguments at the protobuf boundary.
func mapSessionCommand(command *uipb.OpenResponse) (domainui.Command, bool, error) {
	switch {
	case command.GetCreateSession() != nil:
		return emptySessionCommand(domainui.CommandCreateSession), true, nil
	case command.GetListSessions() != nil:
		return emptySessionCommand(domainui.CommandListSessions), true, nil
	case command.GetGetSessionInfo() != nil:
		return emptySessionCommand(domainui.CommandGetSessionInfo), true, nil
	case command.GetResumeSession() != nil:
		resume := command.GetResumeSession()
		if !resume.HasSessionId() || resume.GetSessionId() == "" {
			return domainui.Command{}, true, errors.New("receive UI command: session ID is required")
		}
		mapped := emptySessionCommand(domainui.CommandResumeSession)
		mapped.SessionID = mo.Some(resume.GetSessionId())
		return mapped, true, nil
	case command.GetSetSessionName() != nil:
		name := command.GetSetSessionName()
		if !name.HasName() {
			return domainui.Command{}, true, errors.New("receive UI command: session name is required")
		}
		mapped := emptySessionCommand(domainui.CommandSetSessionName)
		mapped.SessionName = mo.Some(name.GetName())
		return mapped, true, nil
	default:
		return domainui.Command{}, false, nil
	}
}

// emptySessionCommand initializes absent arguments for lifecycle commands without payloads.
func emptySessionCommand(kind domainui.CommandKind) domainui.Command {
	return domainui.Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
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
			text, present := content.Text.Get()
			if !present {
				return nil, false
			}
			//nolint:exhaustruct_v5 // uipb.ToolResultContent_builder sets only the active Text field.
			return uipb.ToolResultContent_builder{
				Text: &text,
			}.Build(), true
		case tool.ResultContentImage:
			image, present := content.Image.Get()
			if !present {
				return nil, false
			}
			mappedImage := uipb.ToolResultImage_builder{
				MediaType: &image.MediaType,
				Data:      nil,
			}.Build()
			mappedImage.SetData(bytes.Clone(image.Data))
			//nolint:exhaustruct_v5 // uipb.ToolResultContent_builder sets only the active Image field.
			return uipb.ToolResultContent_builder{
				Image: mappedImage,
			}.Build(), true
		}
		return nil, false
	})
}

func mapToolCallPreview(preview domainui.ToolCallPreview) (*uipb.ToolCallPreview, error) {
	fields := make([]*uipb.ToolCallPreviewField, 0, len(preview.Fields))
	for _, field := range preview.Fields {
		mapped := uipb.ToolCallPreviewField_builder{
			Name:   new(field.Name),
			Value:  nil,
			Prefix: nil,
		}.Build()
		if field.Complete {
			value, present := field.Value.Get()
			if !present {
				return nil, errors.New("map UI tool call preview: complete value is required")
			}
			protobufValue, err := structpb.NewValue(value)
			if err != nil {
				return nil, fmt.Errorf("map UI tool call preview value: %w", err)
			}
			mapped.SetValue(proto.ValueOrDefault(protobufValue))
		} else {
			prefix, present := field.Prefix.Get()
			if !present {
				return nil, errors.New("map UI tool call preview: prefix is required")
			}
			mapped.SetPrefix(prefix)
		}
		fields = append(fields, mapped)
	}
	return uipb.ToolCallPreview_builder{
		CallId:      new(preview.CallID),
		Name:        new(preview.Name),
		Position:    new(int32(preview.Position)), //nolint:gosec // Positions are bounded by response size.
		Provisional: new(preview.Provisional),
		Fields:      fields,
	}.Build(), nil
}

func mapModelResponse(response domainui.ModelResponse) (*uipb.ModelResponse, error) {
	content, err := lo.MapErr(
		response.Content,
		func(item domainui.ModelResponseContent, _ int) (*uipb.ModelResponseContent, error) {
			var call *uipb.FinalToolCall
			if value, present := item.ToolCall.Get(); present {
				arguments, mapErr := structpb.NewStruct(value.Arguments)
				if mapErr != nil {
					return nil, fmt.Errorf("map restored tool call arguments: %w", mapErr)
				}
				position := int32(value.Position) //nolint:gosec // Response content bounds the position.
				call = uipb.FinalToolCall_builder{
					CallId: new(value.CallID), Name: new(value.Name),
					Position: new(position), Arguments: arguments,
				}.Build()
			}
			return uipb.ModelResponseContent_builder{
				Kind: new(mapModelContentKind(item.Kind)), Text: new(item.Text), ToolCall: call,
			}.Build(), nil
		},
	)
	if err != nil {
		return nil, err
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic domainui.ModelDiagnostic, _ int) *uipb.ModelDiagnostic {
		return uipb.ModelDiagnostic_builder{
			Code:    new(diagnostic.Code),
			Message: new(diagnostic.Message),
		}.Build()
	})
	var outcome *string
	if value, present := response.Outcome.Get(); present {
		outcome = new(value)
	}
	var errorMessage *string
	if value, present := response.ErrorMessage.Get(); present {
		errorMessage = new(value)
	}
	var provider *string
	if value, present := response.Provider.Get(); present {
		provider = new(value)
	}
	var configuredModel *string
	if value, present := response.Model.Get(); present {
		configuredModel = new(value)
	}
	var responseModel *string
	if value, present := response.ResponseModel.Get(); present {
		responseModel = new(value)
	}
	var responseID *string
	if value, present := response.ResponseID.Get(); present {
		responseID = new(value)
	}
	var usage *uipb.ModelUsage
	if value, present := response.Usage.Get(); present {
		usage = uipb.ModelUsage_builder{
			InputTokens:       new(value.InputTokens),
			OutputTokens:      new(value.OutputTokens),
			CachedInputTokens: new(value.CachedInputTokens),
			CacheWriteTokens:  new(value.CacheWriteTokens),
			ReasoningTokens:   new(value.ReasoningTokens),
			TotalTokens:       new(value.TotalTokens),
		}.Build()
	}
	return uipb.ModelResponse_builder{
		Text:          new(response.Text),
		Outcome:       outcome,
		ErrorMessage:  errorMessage,
		Provider:      provider,
		Model:         configuredModel,
		ResponseModel: responseModel,
		ResponseId:    responseID,
		Usage:         usage,
		Diagnostics:   diagnostics,
		Content:       content,
	}.Build(), nil
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
