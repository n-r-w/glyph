package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

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
	var mapErr error
	switch event.Type {
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart:
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd, run.EventMessageEnd:
		mapErr = mapUIModelEvent(event, &lifecycle)
	case run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult:
		mapErr = mapUIToolEvent(event, &lifecycle)
	case run.EventTurnEnd, run.EventAgentEnd:
		mapErr = mapUITerminalEvent(event, &lifecycle)
	}
	if mapErr != nil {
		return mapErr
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

// mapUIModelEvent maps selected model payloads to the UI lifecycle contract.
func mapUIModelEvent(event run.Event, lifecycle *domainui.Lifecycle) error {
	switch event.Type {
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd:
		return mapContentLifecycle(event, lifecycle)
	case run.EventMessageEnd:
		message, present := event.Message.Get()
		if !present {
			return errors.New("deliver UI agent event: message end event requires model response")
		}
		lifecycle.ModelResponse = mapModelResponse(message)
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd,
		run.EventToolResult, run.EventTurnEnd, run.EventAgentEnd:
		return fmt.Errorf("deliver UI agent event: unsupported model event type %d", event.Type)
	}
	return nil
}

// mapContentLifecycle maps one selected Agent Core content payload to the UI contract.
func mapContentLifecycle(event run.Event, lifecycle *domainui.Lifecycle) error {
	content, hasContent := event.Content.Get()
	position, hasPosition := event.Position.Get()
	if !hasContent || !hasPosition {
		return fmt.Errorf("deliver UI agent event: event type %d requires content and position", event.Type)
	}
	contentType := domainui.ModelContentStart
	if event.Type == run.EventTextDelta {
		contentType = domainui.ModelContentTextDelta
	}
	if event.Type == run.EventContentEnd {
		contentType = domainui.ModelContentEnd
	}
	lifecycle.ModelContent = domainui.ModelContent{
		Type: contentType, Kind: modelContentKind(content.Kind), Position: position, Text: "",
	}
	if event.Type == run.EventTextDelta {
		text, present := content.Text.Get()
		if !present {
			return errors.New("deliver UI agent event: text delta event requires text")
		}
		lifecycle.ModelContent.Text = text
	}
	return nil
}

// mapUIToolEvent maps selected tool payloads to the UI lifecycle contract.
func mapUIToolEvent(event run.Event, lifecycle *domainui.Lifecycle) error {
	switch event.Type {
	case run.EventToolCallStart, run.EventToolCallDelta:
		preview, present := event.Preview.Get()
		if !present {
			return fmt.Errorf("deliver UI agent event: event type %d requires tool call preview", event.Type)
		}
		lifecycle.ToolCallPreview = mapToolCallPreview(preview)
	case run.EventToolCallEnd:
		call, hasCall := event.ToolCall.Get()
		position, hasPosition := event.Position.Get()
		if !hasCall || !hasPosition {
			return errors.New("deliver UI agent event: tool call end event requires tool call and position")
		}
		lifecycle.FinalToolCall = domainui.FinalToolCall{
			CallID: call.ID, Name: call.Name, Position: position, Arguments: maps.Clone(call.Arguments),
		}
	case run.EventToolExecutionStart:
		call, present := event.ToolCall.Get()
		if !present {
			return errors.New("deliver UI agent event: tool execution start event requires tool call")
		}
		lifecycle.ToolCallID = call.ID
		lifecycle.ToolName = call.Name
	case run.EventToolExecutionUpdate:
		progress, present := event.Progress.Get()
		if !present {
			return errors.New("deliver UI agent event: tool execution update event requires progress")
		}
		lifecycle.Text = progress.Content
		lifecycle.ProgressChannel = progressChannel(progress.Channel)
	case run.EventToolExecutionEnd, run.EventToolResult:
		result, present := event.ToolResult.Get()
		if !present {
			return fmt.Errorf("deliver UI agent event: event type %d requires tool result", event.Type)
		}
		lifecycle.ToolCallID = result.CallID
		lifecycle.ToolName = result.ToolName
		lifecycle.ToolResultContents = cloneResultContents(result.Contents)
		lifecycle.IsError = result.IsError
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventContentStart, run.EventTextDelta, run.EventContentEnd, run.EventMessageEnd,
		run.EventTurnEnd, run.EventAgentEnd:
		return fmt.Errorf("deliver UI agent event: unsupported tool event type %d", event.Type)
	}
	return nil
}

// mapUITerminalEvent maps selected summaries to the UI lifecycle contract.
func mapUITerminalEvent(event run.Event, lifecycle *domainui.Lifecycle) error {
	switch event.Type {
	case run.EventTurnEnd:
		turn, present := event.Turn.Get()
		if !present {
			return errors.New("deliver UI agent event: turn end event requires turn summary")
		}
		lifecycle.Text = responseText(turn.Response)
		if outcome, hasOutcome := turn.Response.Outcome.Get(); hasOutcome {
			lifecycle.Outcome = modelOutcome(outcome)
		}
		if errorMessage, hasErrorMessage := turn.Response.ErrorMessage.Get(); hasErrorMessage {
			lifecycle.ErrorMessage = errorMessage
		}
	case run.EventAgentEnd:
		summary, present := event.Agent.Get()
		if !present {
			return errors.New("deliver UI agent event: agent end event requires agent summary")
		}
		lifecycle.Outcome = runOutcome(summary.Outcome)
		if errorMessage, hasErrorMessage := summary.ErrorMessage.Get(); hasErrorMessage {
			lifecycle.ErrorMessage = errorMessage
		}
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventContentStart, run.EventTextDelta, run.EventContentEnd,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd, run.EventMessageEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult:
		return fmt.Errorf("deliver UI agent event: unsupported terminal event type %d", event.Type)
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
		image, ok := cloned[index].Image.Get()
		if !ok {
			continue
		}
		image.Data = bytes.Clone(image.Data)
		cloned[index].Image = mo.Some(image)
	}
	return cloned
}

// mapModelResponse copies typed terminal data while excluding opaque provider context.
func mapModelResponse(response model.Response) domainui.ModelResponse {
	content := lo.FilterMap(response.Content, func(item model.Content, _ int) (domainui.ModelResponseContent, bool) {
		kind := modelContentKind(item.Kind)
		text, present := item.Text.Get()
		return domainui.ModelResponseContent{Kind: kind, Text: text}, kind != 0 && present
	})
	var responseModel *string
	if actualModel, ok := response.ResponseModel.Get(); ok {
		value := string(actualModel)
		responseModel = &value
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic model.Diagnostic, _ int) domainui.ModelDiagnostic {
		return domainui.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
	mapped := domainui.ModelResponse{
		Text: "", Outcome: "", ErrorMessage: "", Provider: "", Model: "",
		ResponseModel: responseModel, ResponseID: "", Content: content, Usage: domainui.ModelUsage{},
		Diagnostics: diagnostics,
	}
	mapped.Text = responseText(response)
	if outcome, present := response.Outcome.Get(); present {
		mapped.Outcome = modelOutcome(outcome)
	}
	if errorMessage, present := response.ErrorMessage.Get(); present {
		mapped.ErrorMessage = errorMessage
	}
	if provider, present := response.Provider.Get(); present {
		mapped.Provider = string(provider)
	}
	if configuredModel, present := response.Model.Get(); present {
		mapped.Model = string(configuredModel)
	}
	if responseID, present := response.ResponseID.Get(); present {
		mapped.ResponseID = responseID
	}
	if usage, present := response.Usage.Get(); present {
		mapped.Usage = domainui.ModelUsage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CachedInputTokens: usage.CachedInputTokens, CacheWriteTokens: usage.CacheWriteTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
		}
	}
	return mapped
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
			if text, present := item.Text.Get(); present {
				builder.WriteString(text)
			}
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
