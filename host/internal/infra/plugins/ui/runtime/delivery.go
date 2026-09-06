package runtime

import (
	"context"
	"errors"
	"fmt"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"

	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

var (
	_ events.ClientDelivery      = (*Service)(nil)
	_ runcontrol.SettledDelivery = (*Service)(nil)
)

// ReportRuntimeFailure sends one classified post-start extension failure.
func (d *Service) ReportRuntimeFailure(_ context.Context, failure extension.RuntimeFailure) error {
	message, err := failure.Message()
	if err != nil {
		return fmt.Errorf("format extension runtime failure: %w", err)
	}
	if sendErr := d.ReportError(controllerui.FailureCodeExtension, message); sendErr != nil {
		return fmt.Errorf("send extension runtime failure: %w", sendErr)
	}
	return nil
}

// DeliverAgent filters one Agent Core event into an explicit UI-safe lifecycle frame.
func (d *Service) DeliverAgent(ctx context.Context, event agent.Event) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deliver UI agent event: %w", err)
	}
	lifecycle := controllerui.Lifecycle{
		Type:               mapEventType(event.Type),
		RunID:              mo.Some(event.RunID),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[controllerui.ModelContent](),
		ModelResponse:      mo.None[controllerui.ModelResponse](),
		ToolCallPreview:    mo.None[controllerui.ToolCallPreview](),
		FinalToolCall:      mo.None[controllerui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[controllerui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
	}
	var mapErr error
	switch event.Type {
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart:
	case agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd, agent.EventMessageEnd:
		mapErr = mapUIModelEvent(event, &lifecycle)
	case agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd,
		agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd, agent.EventToolResult:
		mapErr = mapUIToolEvent(event, &lifecycle)
	case agent.EventTurnEnd, agent.EventAgentEnd:
		mapErr = mapUITerminalEvent(event, &lifecycle)
	}
	if mapErr != nil {
		return mapErr
	}
	if err := d.sendFrame(lifecycleFrame(lifecycle)); err != nil {
		return fmt.Errorf("deliver UI agent event: %w", err)
	}
	return nil
}

// DeliverSettled confirms Host settlement without duplicating the operation terminal result.
func (*Service) DeliverSettled(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deliver UI agent settlement: %w", err)
	}
	return nil
}

// PresentAuthorizationURL sends the OAuth URL before any best-effort browser launch.
func (d *Service) PresentAuthorizationURL(ctx context.Context, authorizationURL string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("present UI authorization URL: %w", err)
	}
	if err := d.sendFrame(authorizationFrame(authorizationURL)); err != nil {
		return fmt.Errorf("present UI authorization URL: %w", err)
	}
	return nil
}

// mapUIModelEvent maps selected model payloads to the UI lifecycle contract.
func mapUIModelEvent(event agent.Event, lifecycle *controllerui.Lifecycle) error {
	switch event.Type {
	case agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd:
		return mapContentLifecycle(event, lifecycle)
	case agent.EventMessageEnd:
		message, present := event.Message.Get()
		if !present {
			return errors.New("deliver UI agent event: message end event requires model response")
		}
		response, err := controllerui.ProjectModelResponse(message, false)
		if err != nil {
			return err
		}
		lifecycle.ModelResponse = mo.Some(response)
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart,
		agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd,
		agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd,
		agent.EventToolResult, agent.EventTurnEnd, agent.EventAgentEnd:
		return fmt.Errorf("deliver UI agent event: unsupported model event type %d", event.Type)
	}
	return nil
}

// mapContentLifecycle maps one selected Agent Core content payload to the UI contract.
func mapContentLifecycle(event agent.Event, lifecycle *controllerui.Lifecycle) error {
	content, hasContent := event.Content.Get()
	position, hasPosition := event.Position.Get()
	if !hasContent || !hasPosition {
		return fmt.Errorf("deliver UI agent event: event type %d requires content and position", event.Type)
	}
	contentType := controllerui.ModelContentStart
	if event.Type == agent.EventTextDelta {
		contentType = controllerui.ModelContentTextDelta
	}
	if event.Type == agent.EventContentEnd {
		contentType = controllerui.ModelContentEnd
	}
	text := mo.None[string]()
	if event.Type == agent.EventTextDelta {
		value, present := content.Text.Get()
		if !present {
			return errors.New("deliver UI agent event: text delta event requires text")
		}
		text = mo.Some(value)
	}
	lifecycle.ModelContent = mo.Some(controllerui.ModelContent{
		Type: contentType, Kind: controllerui.ModelContentKindFromDomain(content.Kind), Position: position, Text: text,
	})
	return nil
}

// mapUIToolEvent maps selected tool payloads to the UI lifecycle contract.
func mapUIToolEvent(event agent.Event, lifecycle *controllerui.Lifecycle) error {
	switch event.Type {
	case agent.EventToolCallStart, agent.EventToolCallDelta:
		preview, present := event.Preview.Get()
		if !present {
			return fmt.Errorf("deliver UI agent event: event type %d requires tool call preview", event.Type)
		}
		lifecycle.ToolCallPreview = mo.Some(projectToolCallPreview(preview))
	case agent.EventToolCallEnd:
		call, hasCall := event.ToolCall.Get()
		position, hasPosition := event.Position.Get()
		if !hasCall || !hasPosition {
			return errors.New("deliver UI agent event: tool call end event requires tool call and position")
		}
		lifecycle.FinalToolCall = mo.Some(controllerui.FinalToolCall{
			CallID: call.ID, Name: call.Name, Position: position, Arguments: call.Clone().Arguments,
		})
	case agent.EventToolExecutionStart:
		call, present := event.ToolCall.Get()
		if !present {
			return errors.New("deliver UI agent event: tool execution start event requires tool call")
		}
		lifecycle.ToolCallID = mo.Some(call.ID)
		lifecycle.ToolName = mo.Some(call.Name)
	case agent.EventToolExecutionUpdate:
		progress, present := event.Progress.Get()
		if !present {
			return errors.New("deliver UI agent event: tool execution update event requires progress")
		}
		lifecycle.Text = mo.Some(progress.Content)
		lifecycle.ProgressChannel = mo.Some(progressChannel(progress.Channel))
	case agent.EventToolExecutionEnd, agent.EventToolResult:
		result, present := event.ToolResult.Get()
		if !present {
			return fmt.Errorf("deliver UI agent event: event type %d requires tool result", event.Type)
		}
		lifecycle.ToolCallID = mo.Some(result.CallID)
		lifecycle.ToolName = mo.Some(result.ToolName)
		lifecycle.ToolResultContents = mo.Some(result.Clone().Contents)
		lifecycle.IsError = mo.Some(result.IsError)
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart,
		agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd, agent.EventMessageEnd,
		agent.EventTurnEnd, agent.EventAgentEnd:
		return fmt.Errorf("deliver UI agent event: unsupported tool event type %d", event.Type)
	}
	return nil
}

// mapUITerminalEvent maps selected summaries to the UI lifecycle contract.
func mapUITerminalEvent(event agent.Event, lifecycle *controllerui.Lifecycle) error {
	switch event.Type {
	case agent.EventTurnEnd:
		turn, present := event.Turn.Get()
		if !present {
			return errors.New("deliver UI agent event: turn end event requires turn summary")
		}
		if err := turn.Response.ValidateTerminalContent(); err != nil {
			return fmt.Errorf("deliver UI turn end: %w", err)
		}
		lifecycle.Text = mo.Some(turn.Response.Text())
		if outcome, hasOutcome := turn.Response.Outcome.Get(); hasOutcome {
			lifecycle.Outcome = mo.Some(controllerui.ModelOutcomeText(outcome))
		}
		if errorMessage, hasErrorMessage := turn.Response.ErrorMessage.Get(); hasErrorMessage {
			lifecycle.ErrorMessage = mo.Some(errorMessage)
		}
	case agent.EventAgentEnd:
		summary, present := event.Agent.Get()
		if !present {
			return errors.New("deliver UI agent event: agent end event requires agent summary")
		}
		lifecycle.Outcome = mo.Some(runOutcome(summary.Outcome))
		if errorMessage, hasErrorMessage := summary.ErrorMessage.Get(); hasErrorMessage {
			lifecycle.ErrorMessage = mo.Some(errorMessage)
		}
	case agent.EventAgentStart, agent.EventTurnStart, agent.EventMessageStart,
		agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd,
		agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd, agent.EventMessageEnd,
		agent.EventToolExecutionStart, agent.EventToolExecutionUpdate, agent.EventToolExecutionEnd, agent.EventToolResult:
		return fmt.Errorf("deliver UI agent event: unsupported terminal event type %d", event.Type)
	}
	return nil
}

// mapEventType converts Agent Core event identity without relying on enum positions.
//
//nolint:gocyclo // The flat switch maps the closed Agent Core event set.
func mapEventType(eventType agent.EventType) controllerui.LifecycleType {
	switch eventType {
	case agent.EventAgentStart:
		return controllerui.LifecycleAgentStart
	case agent.EventTurnStart:
		return controllerui.LifecycleTurnStart
	case agent.EventMessageStart:
		return controllerui.LifecycleMessageStart
	case agent.EventContentStart:
		return controllerui.LifecycleModelContentStart
	case agent.EventTextDelta:
		return controllerui.LifecycleModelTextDelta
	case agent.EventContentEnd:
		return controllerui.LifecycleModelContentEnd
	case agent.EventToolCallStart:
		return controllerui.LifecycleToolCallStart
	case agent.EventToolCallDelta:
		return controllerui.LifecycleToolCallDelta
	case agent.EventToolCallEnd:
		return controllerui.LifecycleToolCallEnd
	case agent.EventMessageEnd:
		return controllerui.LifecycleMessageEnd
	case agent.EventToolExecutionStart:
		return controllerui.LifecycleToolExecutionStart
	case agent.EventToolExecutionUpdate:
		return controllerui.LifecycleToolExecutionUpdate
	case agent.EventToolExecutionEnd:
		return controllerui.LifecycleToolExecutionEnd
	case agent.EventToolResult:
		return controllerui.LifecycleToolResult
	case agent.EventTurnEnd:
		return controllerui.LifecycleTurnEnd
	case agent.EventAgentEnd:
		return controllerui.LifecycleAgentEnd
	default:
		return 0
	}
}

// projectToolCallPreview copies provisional tool fields into operation progress.
func projectToolCallPreview(preview model.ToolCallPreview) controllerui.ToolCallPreview {
	fields := lo.Map(preview.Fields, func(field model.ToolCallPreviewField, _ int) controllerui.ToolCallPreviewField {
		mapped := controllerui.ToolCallPreviewField{
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
	return controllerui.ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

// runOutcome maps a terminal run outcome to one stable UI value.
func runOutcome(outcome agent.RunOutcome) string {
	switch outcome {
	case agent.RunOutcomeCompleted:
		return controllerui.OutcomeTextCompleted
	case agent.RunOutcomeAborted:
		return controllerui.OutcomeTextAborted
	case agent.RunOutcomeFailed:
		return controllerui.OutcomeTextFailed
	default:
		return ""
	}
}

// progressChannel maps tool progress identity to the UI contract.
func progressChannel(channel tool.ProgressChannel) controllerui.ProgressChannel {
	switch channel {
	case tool.ProgressChannelStatus:
		return controllerui.ProgressChannelStatus
	case tool.ProgressChannelStdout:
		return controllerui.ProgressChannelStdout
	case tool.ProgressChannelStderr:
		return controllerui.ProgressChannelStderr
	default:
		return 0
	}
}

var _ extensionruntime.FailureReporter = (*Service)(nil)
