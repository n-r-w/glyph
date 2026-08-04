package ui

import (
	"context"
	"fmt"
	"strings"

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
	lifecycle := domainui.Lifecycle{
		Type:            mapEventType(event.Type),
		RunID:           event.RunID,
		Position:        event.Position,
		Text:            "",
		ToolCallID:      "",
		ToolName:        "",
		ProgressChannel: 0,
		IsError:         false,
		Outcome:         "",
		ErrorMessage:    "",
		Availability:    0,
	}
	switch event.Type {
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart:
	case run.EventMessageUpdate:
		lifecycle.Text = event.Delta
	case run.EventMessageEnd:
		lifecycle.Text = responseText(event.Message)
		lifecycle.Outcome = modelOutcome(event.Message.Outcome)
		lifecycle.ErrorMessage = event.Message.ErrorMessage
	case run.EventToolExecutionStart:
		lifecycle.ToolCallID = event.ToolCall.ID
		lifecycle.ToolName = event.ToolCall.Name
	case run.EventToolExecutionUpdate:
		lifecycle.Text = event.Progress.Content
		lifecycle.ProgressChannel = progressChannel(event.Progress.Channel)
	case run.EventToolExecutionEnd, run.EventToolResult:
		lifecycle.ToolCallID = event.ToolResult.CallID
		lifecycle.ToolName = event.ToolResult.ToolName
		lifecycle.Text = event.ToolResult.Content
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

// mapEventType converts Agent Core event identity without copying event payload objects.
func mapEventType(eventType run.EventType) domainui.LifecycleType {
	return domainui.LifecycleType(eventType)
}

// responseText joins only public model text items and drops opaque provider context.
func responseText(response agent.ModelResponse) string {
	var builder strings.Builder
	for _, item := range response.Items {
		if item.Kind == agent.ModelItemText {
			builder.WriteString(item.Text)
		}
	}
	return builder.String()
}

// modelOutcome maps a terminal model outcome to one stable UI value.
func modelOutcome(outcome agent.ModelOutcome) string {
	switch outcome {
	case agent.ModelOutcomeStop:
		return "stop"
	case agent.ModelOutcomeToolUse:
		return "tool_use"
	case agent.ModelOutcomeLength:
		return "length"
	case agent.ModelOutcomeAborted:
		return "aborted"
	case agent.ModelOutcomeFailed:
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
