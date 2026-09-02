package runtime

import (
	"errors"
	"fmt"
	"slices"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/session"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapInitialization converts one complete startup state.
func mapInitialization(initialization domainui.Initialization) (*uiv1.Initialization, error) {
	startup := lo.Map(initialization.StartupContent, func(content domainui.StartupContent, _ int) *uiv1.StartupContent {
		return uiv1.StartupContent_builder{
			Severity: new(mapSeverity(content.Severity)),
			Text:     new(content.Text),
		}.Build()
	})
	extensions := lo.Map(
		initialization.Extensions,
		func(extension domainui.ExtensionAvailability, _ int) *uiv1.ExtensionAvailability {
			return uiv1.ExtensionAvailability_builder{
				PluginId: new(extension.PluginID),
				Tools:    slices.Clone(extension.Tools),
				Path:     new(extension.Path),
			}.Build()
		},
	)
	models := lo.Map(initialization.Models, func(configured domainui.ConfiguredModel, _ int) *uiv1.ConfiguredModel {
		choices := lo.Map(
			configured.Reasoning.Choices,
			func(choice domainui.ReasoningChoice, _ int) uiv1.ReasoningChoice {
				return mapReasoningChoice(choice)
			},
		)
		reasoning := uiv1.ReasoningCapabilities_builder{
			Supported:     new(configured.Reasoning.Supported),
			Choices:       choices,
			DefaultChoice: new(mapReasoningChoice(configured.Reasoning.Default)),
		}.Build()
		return uiv1.ConfiguredModel_builder{
			ProviderId: new(configured.ProviderID),
			ModelId:    new(configured.ModelID),
			Reasoning:  reasoning,
		}.Build()
	})
	selection, present := initialization.ModelSelection.Get()
	if !present {
		return nil, errors.New("map UI initialization: model selection is required")
	}
	return uiv1.Initialization_builder{
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
func mapModelSelection(selection domainui.ModelSelection) *uiv1.ModelSelection {
	return uiv1.ModelSelection_builder{
		ProviderId:      new(selection.ProviderID),
		ModelId:         new(selection.ModelID),
		ReasoningChoice: new(mapReasoningChoice(selection.ReasoningChoice)),
	}.Build()
}

// mapLifecycle converts one explicit lifecycle payload.
func mapLifecycle(event domainui.Lifecycle) (*uiv1.AgentEvent, error) {
	mapped := mapLifecycleScalars(event)
	if event.Type != domainui.LifecycleAvailabilityChanged && event.RunID.IsNone() {
		return nil, errors.New("map UI lifecycle: run ID is required")
	}
	switch event.Type {
	case domainui.LifecycleAgentStart, domainui.LifecycleTurnStart, domainui.LifecycleMessageStart:
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
func mapLifecycleScalars(event domainui.Lifecycle) *uiv1.AgentEvent {
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
	var mappedProgressChannel *uiv1.ProgressChannel
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
	var availability *uiv1.Availability
	if value, present := event.Availability.Get(); present {
		availability = new(mapAvailability(value))
	}
	return uiv1.AgentEvent_builder{
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
func mapModelLifecycle(event domainui.Lifecycle, mapped *uiv1.AgentEvent) (*uiv1.AgentEvent, error) {
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
	mapped.SetModelContent(uiv1.ModelContent_builder{
		Type:     new(mapModelContentType(content.Type)),
		Position: new(int64(content.Position)),
		Text:     contentText,
		Kind:     new(mapModelContentKind(content.Kind)),
	}.Build())
	return mapped, nil
}

// mapToolCallLifecycle validates and maps selected tool-call payloads.
func mapToolCallLifecycle(event domainui.Lifecycle, mapped *uiv1.AgentEvent) (*uiv1.AgentEvent, error) {
	if event.Type == domainui.LifecycleToolCallEnd {
		call, present := event.FinalToolCall.Get()
		if !present {
			return nil, errors.New("map UI lifecycle: final tool call is required")
		}
		arguments, err := structpb.NewStruct(call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("map UI lifecycle final tool call: %w", err)
		}
		mapped.SetFinalToolCall(uiv1.FinalToolCall_builder{
			CallId:    new(call.CallID),
			Name:      new(call.Name),
			Position:  new(int64(call.Position)),
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
func mapToolExecutionLifecycle(event domainui.Lifecycle, mapped *uiv1.AgentEvent) (*uiv1.AgentEvent, error) {
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
		domainui.LifecycleAvailabilityChanged:
		return nil, fmt.Errorf("map UI lifecycle: unsupported tool execution event type %d", event.Type)
	}
	return mapped, nil
}

// mapTerminalLifecycle validates selected turn and agent summaries.
func mapTerminalLifecycle(event domainui.Lifecycle, mapped *uiv1.AgentEvent) (*uiv1.AgentEvent, error) {
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
		domainui.LifecycleAvailabilityChanged:
		return nil, fmt.Errorf("map UI lifecycle: unsupported terminal event type %d", event.Type)
	}
	return mapped, nil
}

// mapSessionInfo preserves absent name and storage path through protobuf scalar presence.
func mapSessionInfo(info session.Info) *uiv1.SessionInfo {
	wire := uiv1.SessionInfo_builder{
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
func mapSessionStatistics(statistics session.Statistics) *uiv1.SessionStatistics {
	wire := uiv1.SessionStatistics_builder{
		UserMessages: new(int64(statistics.UserMessages)), ModelResponses: new(int64(statistics.ModelResponses)),
		ToolCalls: new(int64(statistics.ToolCalls)), ToolResults: new(int64(statistics.ToolResults)),
		TotalMessages: new(int64(statistics.TotalMessages)), Tokens: nil,
		EstimatedCost: nil, CostBreakdown: nil,
	}.Build()
	if usage, present := statistics.TokenUsage.Get(); present {
		wire.SetTokens(uiv1.TokenUsage_builder{
			InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
			CacheReadTokens: new(usage.CacheReadTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
			ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
		}.Build())
	}
	if cost, present := statistics.EstimatedCost.Get(); present {
		wire.SetEstimatedCost(mapEstimatedCost(cost))
	}
	breakdown := lo.Map(statistics.CostBreakdown, func(group session.ProviderModelCost, _ int) *uiv1.ProviderModelCost {
		mapped := uiv1.ProviderModelCost_builder{
			ProviderId: new(string(group.Provider)), ModelId: new(string(group.Model)), EstimatedCost: nil,
		}.Build()
		if cost, present := group.EstimatedCost.Get(); present {
			mapped.SetEstimatedCost(mapEstimatedCost(cost))
		}
		return mapped
	})
	wire.SetCostBreakdown(breakdown)
	return wire
}

// mapEstimatedCost preserves calculated cost buckets and their stored total.
func mapEstimatedCost(cost session.EstimatedCost) *uiv1.EstimatedCost {
	return uiv1.EstimatedCost_builder{
		Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
		CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
	}.Build()
}

// mapSessionSummary preserves optional row labels while mapping an ordered list entry.
func mapSessionSummary(summary session.Summary) *uiv1.SessionSummary {
	wire := uiv1.SessionSummary_builder{
		Info:          mapSessionInfo(summary.Info),
		TotalMessages: new(int64(summary.TotalMessages)), FirstUserText: nil,
	}.Build()
	if text, present := summary.FirstUserText.Get(); present {
		wire.SetFirstUserText(text)
	}
	return wire
}
