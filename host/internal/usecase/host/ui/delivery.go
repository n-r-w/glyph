package ui

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// Delivery maps ordered Host and Agent events to one UI stream.
type Delivery struct {
	channel Channel
}

// NewDelivery creates one synchronous UI delivery path.
func NewDelivery(channel Channel) *Delivery {
	return &Delivery{channel: channel}
}

// ReportRuntimeFailure sends one classified post-start extension failure.
func (d *Delivery) ReportRuntimeFailure(_ context.Context, failure tool.RuntimeFailure) error {
	message, err := failure.Message()
	if err != nil {
		return fmt.Errorf("format extension runtime failure: %w", err)
	}
	if sendErr := d.channel.Send(errorFrame(message, false)); sendErr != nil {
		return fmt.Errorf("send extension runtime failure: %w", sendErr)
	}
	return nil
}

// DeliverAgent filters one Agent Core event into an explicit UI-safe lifecycle frame.
func (d *Delivery) DeliverAgent(ctx context.Context, event run.Event) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deliver UI agent event: %w", err)
	}
	lifecycle := emptyLifecycle()
	lifecycle.Type = mapEventType(event.Type)
	lifecycle.RunID = event.RunID
	switch event.Type {
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart:
	case run.EventContentStart:
		lifecycle.ModelContent = domainui.ModelContent{
			Type: domainui.ModelContentStart, Kind: modelContentKind(event.Content.Kind),
			Position: event.Position, Text: "",
		}
	case run.EventTextDelta:
		lifecycle.ModelContent = domainui.ModelContent{
			Type: domainui.ModelContentTextDelta, Kind: modelContentKind(event.Content.Kind),
			Position: event.Position, Text: event.Content.Text,
		}
	case run.EventContentEnd:
		lifecycle.ModelContent = domainui.ModelContent{
			Type: domainui.ModelContentEnd, Kind: modelContentKind(event.Content.Kind),
			Position: event.Position, Text: "",
		}
	case run.EventToolCallStart, run.EventToolCallDelta:
		lifecycle.ToolCallPreview = mapToolCallPreview(event.Preview)
	case run.EventToolCallEnd:
		lifecycle.FinalToolCall = domainui.FinalToolCall{
			CallID: event.ToolCall.ID, Name: event.ToolCall.Name, Position: event.Position,
			Arguments: maps.Clone(event.ToolCall.Arguments),
		}
	case run.EventMessageEnd:
		lifecycle.ModelResponse = mapModelResponse(event.Message)
	case run.EventToolExecutionStart:
		lifecycle.ToolCallID = event.ToolCall.ID
		lifecycle.ToolName = event.ToolCall.Name
	case run.EventToolExecutionUpdate:
		lifecycle.Text = event.Progress.Content
		lifecycle.ProgressChannel = progressChannel(event.Progress.Channel)
	case run.EventToolExecutionEnd, run.EventToolResult:
		lifecycle.ToolCallID = event.ToolResult.CallID
		lifecycle.ToolName = event.ToolResult.ToolName
		lifecycle.ToolResultContents = cloneResultContents(event.ToolResult.Contents)
		lifecycle.IsError = event.ToolResult.IsError
	case run.EventTurnEnd:
		lifecycle.Text = responseText(event.Turn.Response)
		lifecycle.Outcome = modelOutcome(event.Turn.Response.Outcome)
		lifecycle.ErrorMessage = event.Turn.Response.ErrorMessage
	case run.EventAgentEnd:
		lifecycle.Outcome = runOutcome(event.Agent.Outcome)
		lifecycle.ErrorMessage = event.Agent.ErrorMessage
	}
	if err := d.channel.Send(lifecycleFrame(lifecycle)); err != nil {
		return fmt.Errorf("deliver UI agent event: %w", err)
	}
	return nil
}

// DeliverSettled sends the Host-owned settlement after every Agent recipient completes.
func (d *Delivery) DeliverSettled(ctx context.Context, runID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deliver UI agent settlement: %w", err)
	}
	lifecycle := emptyLifecycle()
	lifecycle.Type = domainui.LifecycleAgentSettled
	lifecycle.RunID = runID
	if err := d.channel.Send(lifecycleFrame(lifecycle)); err != nil {
		return fmt.Errorf("deliver UI agent settlement: %w", err)
	}
	return nil
}

// PresentAuthorizationURL sends the OAuth URL before any best-effort browser launch.
func (d *Delivery) PresentAuthorizationURL(ctx context.Context, authorizationURL string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("present UI authorization URL: %w", err)
	}
	if err := d.channel.Send(authorizationFrame(authorizationURL)); err != nil {
		return fmt.Errorf("present UI authorization URL: %w", err)
	}
	return nil
}

// mapEventType converts Agent Core event identity without relying on enum positions.
//
//nolint:gocyclo // The flat switch maps the closed Agent Core event set.
func mapEventType(eventType run.EventType) domainui.LifecycleType {
	switch eventType {
	case run.EventAgentStart:
		return domainui.LifecycleAgentStart
	case run.EventTurnStart:
		return domainui.LifecycleTurnStart
	case run.EventMessageStart:
		return domainui.LifecycleMessageStart
	case run.EventContentStart:
		return domainui.LifecycleModelContentStart
	case run.EventTextDelta:
		return domainui.LifecycleModelTextDelta
	case run.EventContentEnd:
		return domainui.LifecycleModelContentEnd
	case run.EventToolCallStart:
		return domainui.LifecycleToolCallStart
	case run.EventToolCallDelta:
		return domainui.LifecycleToolCallDelta
	case run.EventToolCallEnd:
		return domainui.LifecycleToolCallEnd
	case run.EventMessageEnd:
		return domainui.LifecycleMessageEnd
	case run.EventToolExecutionStart:
		return domainui.LifecycleToolExecutionStart
	case run.EventToolExecutionUpdate:
		return domainui.LifecycleToolExecutionUpdate
	case run.EventToolExecutionEnd:
		return domainui.LifecycleToolExecutionEnd
	case run.EventToolResult:
		return domainui.LifecycleToolResult
	case run.EventTurnEnd:
		return domainui.LifecycleTurnEnd
	case run.EventAgentEnd:
		return domainui.LifecycleAgentEnd
	default:
		return 0
	}
}

func mapToolCallPreview(preview model.ToolCallPreview) domainui.ToolCallPreview {
	fields := lo.Map(preview.Fields, func(field model.ToolCallPreviewField, _ int) domainui.ToolCallPreviewField {
		return domainui.ToolCallPreviewField{
			Name: field.Name, Value: field.Value, Prefix: field.Prefix,
			Complete: field.Kind == model.ToolCallPreviewFieldComplete,
		}
	})
	return domainui.ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

// cloneResultContents isolates mutable image bytes before lifecycle delivery.
func cloneResultContents(contents []tool.ResultContent) []tool.ResultContent {
	cloned := slices.Clone(contents)
	for index := range cloned {
		cloned[index].Image.Data = bytes.Clone(cloned[index].Image.Data)
	}
	return cloned
}

// mapModelResponse copies typed terminal data while excluding opaque provider context.
func mapModelResponse(response model.Response) domainui.ModelResponse {
	content := lo.FilterMap(response.Content, func(item model.Content, _ int) (domainui.ModelResponseContent, bool) {
		kind := modelContentKind(item.Kind)
		return domainui.ModelResponseContent{Kind: kind, Text: item.Text}, kind != 0
	})
	var responseModel *string
	if response.ResponseModel != nil {
		value := string(*response.ResponseModel)
		responseModel = &value
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic model.Diagnostic, _ int) domainui.ModelDiagnostic {
		return domainui.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
	return domainui.ModelResponse{
		Text: responseText(response), Outcome: modelOutcome(response.Outcome), ErrorMessage: response.ErrorMessage,
		Provider: string(response.Provider), Model: string(response.Model), ResponseModel: responseModel,
		ResponseID: response.ResponseID,
		Content:    content,
		Usage: domainui.ModelUsage{
			InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
			CachedInputTokens: response.Usage.CachedInputTokens, CacheWriteTokens: response.Usage.CacheWriteTokens,
			ReasoningTokens: response.Usage.ReasoningTokens, TotalTokens: response.Usage.TotalTokens,
		},
		Diagnostics: diagnostics,
	}
}

// modelContentKind maps only UI-safe streamed content kinds.
func modelContentKind(kind model.ContentKind) domainui.ModelContentKind {
	switch kind {
	case model.ContentText:
		return domainui.ModelContentKindText
	case model.ContentRefusal:
		return domainui.ModelContentKindRefusal
	case model.ContentReasoning:
		return domainui.ModelContentKindReasoning
	case model.ContentToolCall:
		return 0
	default:
		return 0
	}
}

// responseText joins only public model text items.
func responseText(response model.Response) string {
	var builder strings.Builder
	for index := range response.Content {
		item := &response.Content[index]
		if item.Kind == model.ContentText || item.Kind == model.ContentRefusal {
			builder.WriteString(item.Text)
		}
	}
	return builder.String()
}

// modelOutcome maps a terminal model outcome to one stable UI value.
func modelOutcome(outcome model.Outcome) string {
	switch outcome {
	case model.OutcomeStop:
		return "stop"
	case model.OutcomeToolUse:
		return "tool_use"
	case model.OutcomeLength:
		return "length"
	case model.OutcomeAborted:
		return "aborted"
	case model.OutcomeFailed:
		return "failed"
	default:
		return ""
	}
}

// runOutcome maps a terminal run outcome to one stable UI value.
func runOutcome(outcome agent.RunOutcome) string {
	switch outcome {
	case agent.RunOutcomeCompleted:
		return "completed"
	case agent.RunOutcomeAborted:
		return "aborted"
	case agent.RunOutcomeFailed:
		return "failed"
	default:
		return ""
	}
}

// progressChannel maps tool progress identity to the UI contract.
func progressChannel(channel tool.ProgressChannel) domainui.ProgressChannel {
	switch channel {
	case tool.ProgressChannelStatus:
		return domainui.ProgressChannelStatus
	case tool.ProgressChannelStdout:
		return domainui.ProgressChannelStdout
	case tool.ProgressChannelStderr:
		return domainui.ProgressChannelStderr
	default:
		return 0
	}
}
