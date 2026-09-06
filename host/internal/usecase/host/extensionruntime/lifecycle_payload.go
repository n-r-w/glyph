package extensionruntime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
)

// Response contains public terminal response facts without provider replay context.
type Response struct {
	// Content contains ordered public model content.
	Content []Content
	// Outcome preserves terminal outcome presence.
	Outcome mo.Option[model.Outcome]
	// ErrorMessage retains complete terminal failure text.
	ErrorMessage mo.Option[string]
	// Provider identifies the requested provider.
	Provider mo.Option[model.ProviderID]
	// Model identifies the configured model.
	Model mo.Option[model.ID]
	// ResponseModel identifies the provider-reported model.
	ResponseModel mo.Option[model.ID]
	// ResponseID identifies the provider response.
	ResponseID mo.Option[string]
	// Usage contains provider-reported token accounting.
	Usage mo.Option[model.Usage]
	// Diagnostics contains complete typed diagnostic details.
	Diagnostics []model.Diagnostic
}

// LifecycleInvocation contains one bound, public lifecycle notification without private run history.
type LifecycleInvocation struct {
	// Context binds the notification to the trusted runtime and active session.
	Context extension.Context
	// Type identifies the source transition.
	Type agent.EventType
	// Settled distinguishes Host settlement from an ordinary agent event.
	Settled bool
	// RunID identifies the agent run.
	RunID string
	// Position preserves response-block position presence.
	Position mo.Option[int]
	// Content contains only public content when visible.
	Content mo.Option[Content]
	// ToolCall contains a finalized provider-neutral call when present.
	ToolCall mo.Option[model.ToolCall]
	// Preview contains provisional public tool-call fields.
	Preview mo.Option[model.ToolCallPreview]
	// Progress contains tool execution progress.
	Progress mo.Option[tool.Progress]
	// ToolResult contains a terminal tool result for tool-end notifications.
	ToolResult mo.Option[agent.ToolResult]
	// Response contains a filtered message or turn response when present.
	Response mo.Option[Response]
	// TurnResults retains ordered terminal tool results for a turn-end notification.
	TurnResults []agent.ToolResult
	// Outcome contains the run outcome for an agent-end notification.
	Outcome mo.Option[agent.RunOutcome]
	// ErrorMessage retains complete agent-end failure text.
	ErrorMessage mo.Option[string]
}

// projectLifecycle removes provider-only content and private terminal history before process encoding.
func (s *Service) projectLifecycle(binding extension.Context, source lifecycle.Event) LifecycleInvocation {
	event := source.Agent
	result := LifecycleInvocation{
		Context: binding, Type: event.Type, Settled: source.Settled, RunID: event.RunID, Position: event.Position,
		Content: mo.None[Content](), ToolCall: event.ToolCall, Preview: event.Preview, Progress: event.Progress,
		ToolResult: event.ToolResult, Response: mo.None[Response](), TurnResults: nil,
		Outcome: mo.None[agent.RunOutcome](), ErrorMessage: mo.None[string](),
	}
	if value, present := event.Content.Get(); present && visibleLifecycleContent(value) {
		result.Content = mo.Some(projectContent(value))
	}
	if value, present := event.Message.Get(); present && event.Type == agent.EventMessageEnd {
		result.Response = mo.Some(projectResponse(value))
	}
	if value, present := event.Turn.Get(); present && event.Type == agent.EventTurnEnd {
		result.Response = mo.Some(projectResponse(value.Response))
		result.TurnResults = value.ToolResults
	}
	if value, present := event.Agent.Get(); present && event.Type == agent.EventAgentEnd {
		result.Outcome = mo.Some(value.Outcome)
		result.ErrorMessage = value.ErrorMessage
	}
	return result
}

// visibleLifecycleContent keeps public text and excludes only provider-context-only reasoning.
func visibleLifecycleContent(content model.Content) bool {
	return content.Kind != model.ContentReasoning || content.Text.IsSome() || content.ProviderContext.IsNone()
}

// projectResponse preserves terminal facts and excludes provider-context-only reasoning blocks.
func projectResponse(response model.Response) Response {
	content := make([]Content, 0, len(response.Content))
	for index := range response.Content {
		if visibleLifecycleContent(response.Content[index]) {
			content = append(content, projectContent(response.Content[index]))
		}
	}
	return Response{
		Content:       content,
		Outcome:       response.Outcome,
		ErrorMessage:  response.ErrorMessage,
		Provider:      response.Provider,
		Model:         response.Model,
		ResponseModel: response.ResponseModel,
		ResponseID:    response.ResponseID,
		Usage:         response.Usage,
		Diagnostics:   response.Diagnostics,
	}
}
