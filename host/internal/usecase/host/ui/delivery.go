package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

const (
	// terminalOutcomeAborted identifies an aborted model request or agent run.
	terminalOutcomeAborted = "aborted"
	// terminalOutcomeFailed identifies a failed model request or agent run.
	terminalOutcomeFailed = "failed"
)

// Delivery maps ordered Host and Agent events to one UI stream.
type Delivery struct {
	// channel sends frames to and receives commands from the selected UI.
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
	lifecycle := domainui.Lifecycle{
		Type:               mapEventType(event.Type),
		RunID:              mo.Some(event.RunID),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.None[domainui.Availability](),
	}
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
	lifecycle := domainui.Lifecycle{
		Type:               domainui.LifecycleAgentSettled,
		RunID:              mo.Some(runID),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.None[domainui.Availability](),
	}
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
		response, err := mapModelResponse(message)
		if err != nil {
			return err
		}
		lifecycle.ModelResponse = mo.Some(response)
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
	text := mo.None[string]()
	if event.Type == run.EventTextDelta {
		value, present := content.Text.Get()
		if !present {
			return errors.New("deliver UI agent event: text delta event requires text")
		}
		text = mo.Some(value)
	}
	lifecycle.ModelContent = mo.Some(domainui.ModelContent{
		Type: contentType, Kind: modelContentKind(content.Kind), Position: position, Text: text,
	})
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
		lifecycle.ToolCallPreview = mo.Some(mapToolCallPreview(preview))
	case run.EventToolCallEnd:
		call, hasCall := event.ToolCall.Get()
		position, hasPosition := event.Position.Get()
		if !hasCall || !hasPosition {
			return errors.New("deliver UI agent event: tool call end event requires tool call and position")
		}
		lifecycle.FinalToolCall = mo.Some(domainui.FinalToolCall{
			CallID: call.ID, Name: call.Name, Position: position, Arguments: call.Clone().Arguments,
		})
	case run.EventToolExecutionStart:
		call, present := event.ToolCall.Get()
		if !present {
			return errors.New("deliver UI agent event: tool execution start event requires tool call")
		}
		lifecycle.ToolCallID = mo.Some(call.ID)
		lifecycle.ToolName = mo.Some(call.Name)
	case run.EventToolExecutionUpdate:
		progress, present := event.Progress.Get()
		if !present {
			return errors.New("deliver UI agent event: tool execution update event requires progress")
		}
		lifecycle.Text = mo.Some(progress.Content)
		lifecycle.ProgressChannel = mo.Some(progressChannel(progress.Channel))
	case run.EventToolExecutionEnd, run.EventToolResult:
		result, present := event.ToolResult.Get()
		if !present {
			return fmt.Errorf("deliver UI agent event: event type %d requires tool result", event.Type)
		}
		lifecycle.ToolCallID = mo.Some(result.CallID)
		lifecycle.ToolName = mo.Some(result.ToolName)
		lifecycle.ToolResultContents = mo.Some(result.Clone().Contents)
		lifecycle.IsError = mo.Some(result.IsError)
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
		if err := turn.Response.ValidateTerminalContent(); err != nil {
			return fmt.Errorf("deliver UI turn end: %w", err)
		}
		lifecycle.Text = mo.Some(turn.Response.Text())
		if outcome, hasOutcome := turn.Response.Outcome.Get(); hasOutcome {
			lifecycle.Outcome = mo.Some(modelOutcome(outcome))
		}
		if errorMessage, hasErrorMessage := turn.Response.ErrorMessage.Get(); hasErrorMessage {
			lifecycle.ErrorMessage = mo.Some(errorMessage)
		}
	case run.EventAgentEnd:
		summary, present := event.Agent.Get()
		if !present {
			return errors.New("deliver UI agent event: agent end event requires agent summary")
		}
		lifecycle.Outcome = mo.Some(runOutcome(summary.Outcome))
		if errorMessage, hasErrorMessage := summary.ErrorMessage.Get(); hasErrorMessage {
			lifecycle.ErrorMessage = mo.Some(errorMessage)
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
		mapped := domainui.ToolCallPreviewField{
			Name: field.Name, Value: mo.None[any](), Prefix: mo.None[string](), Complete: false,
		}
		switch field.Kind {
		case model.ToolCallPreviewFieldComplete:
			mapped.Value = field.Clone().Value
			mapped.Complete = true
		case model.ToolCallPreviewFieldPrefix:
			mapped.Prefix = field.Prefix
		}
		return mapped
	})
	return domainui.ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

// mapModelResponse copies typed terminal data while excluding opaque provider context.
func mapModelResponse(response model.Response) (domainui.ModelResponse, error) {
	return mapModelResponseProjection(response, false)
}

func mapModelResponseProjection(
	response model.Response,
	continuationOnly bool,
) (domainui.ModelResponse, error) {
	if err := response.ValidateTerminalContent(); err != nil {
		return domainui.ModelResponse{}, fmt.Errorf("map UI model response: %w", err)
	}
	mappedContent, err := lo.MapErr(response.Content, func(
		item model.Content,
		position int,
	) (mo.Option[domainui.ModelResponseContent], error) {
		if continuationOnly && item.Kind != model.ContentText && item.Kind != model.ContentToolCall {
			return mo.None[domainui.ModelResponseContent](), nil
		}
		return mapUIModelResponseContent(position, item)
	})
	if err != nil {
		return domainui.ModelResponse{}, err
	}
	content := make([]domainui.ModelResponseContent, 0, len(mappedContent))
	for position := range mappedContent {
		if item, present := mappedContent[position].Get(); present {
			content = append(content, item)
		}
	}
	responseModel := mo.None[string]()
	if actualModel, ok := response.ResponseModel.Get(); ok {
		responseModel = mo.Some(string(actualModel))
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic model.Diagnostic, _ int) domainui.ModelDiagnostic {
		return domainui.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
	outcome := mo.None[string]()
	if value, present := response.Outcome.Get(); present {
		outcome = mo.Some(modelOutcome(value))
	}
	errorMessage := mo.None[string]()
	if value, present := response.ErrorMessage.Get(); present {
		errorMessage = mo.Some(value)
	}
	provider := mo.None[string]()
	if value, present := response.Provider.Get(); present {
		provider = mo.Some(string(value))
	}
	configuredModel := mo.None[string]()
	if value, present := response.Model.Get(); present {
		configuredModel = mo.Some(string(value))
	}
	responseID := mo.None[string]()
	if value, present := response.ResponseID.Get(); present {
		responseID = mo.Some(value)
	}
	mappedUsage := mo.None[domainui.ModelUsage]()
	if usage, present := response.Usage.Get(); present {
		mappedUsage = mo.Some(domainui.ModelUsage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CachedInputTokens: usage.CachedInputTokens, CacheWriteTokens: usage.CacheWriteTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
		})
	}
	return domainui.ModelResponse{
		Text: response.Text(), Outcome: outcome, ErrorMessage: errorMessage,
		Provider: provider, Model: configuredModel, ResponseModel: responseModel,
		ResponseID: responseID, Content: content, Usage: mappedUsage, Diagnostics: diagnostics,
	}, nil
}

// mapUIModelResponseContent projects one valid terminal content item without opaque provider data.
func mapUIModelResponseContent(
	position int,
	item model.Content,
) (mo.Option[domainui.ModelResponseContent], error) {
	if item.Kind == model.ContentToolCall {
		call, present := item.ToolCall.Get()
		if !present {
			return mo.None[domainui.ModelResponseContent](), errors.New("UI model response tool call is missing")
		}
		return mo.Some(domainui.ModelResponseContent{
			Kind: domainui.ModelContentKind(0), Text: "",
			ToolCall: mo.Some(domainui.FinalToolCall{
				CallID: call.ID, Name: call.Name, Position: position, Arguments: call.Clone().Arguments,
			}),
		}), nil
	}
	kind := modelContentKind(item.Kind)
	if kind == 0 {
		return mo.None[domainui.ModelResponseContent](), nil
	}
	text, present := item.Text.Get()
	if !present {
		if item.Kind == model.ContentReasoning && item.ProviderContext.IsSome() {
			return mo.None[domainui.ModelResponseContent](), nil
		}
		return mo.None[domainui.ModelResponseContent](), errors.New("UI model response content text is missing")
	}
	return mo.Some(
		domainui.ModelResponseContent{Kind: kind, Text: text, ToolCall: mo.None[domainui.FinalToolCall]()},
	), nil
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
		return terminalOutcomeAborted
	case model.OutcomeFailed:
		return terminalOutcomeFailed
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
		return terminalOutcomeAborted
	case agent.RunOutcomeFailed:
		return terminalOutcomeFailed
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
