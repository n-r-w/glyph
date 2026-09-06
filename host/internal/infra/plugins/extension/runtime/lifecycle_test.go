//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// emptyLifecycleSource creates one source event with every optional payload absent.
func emptyLifecycleSource(eventType run.EventType) run.Event {
	return run.Event{
		Type: eventType, RunID: "run", Position: mo.None[int](), Content: mo.None[model.Content](),
		Message: mo.None[model.Response](), Preview: mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](), Progress: mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](), Turn: mo.None[run.TurnSummary](), Agent: mo.None[run.AgentSummary](),
	}
}

// TestMapLifecycleEventCoversEveryObserverPayload verifies each approved group has one typed public variant.
func TestMapLifecycleEventCoversEveryObserverPayload(t *testing.T) {
	t.Parallel()

	// Arrange one valid source event for each Agent Core lifecycle group.
	response := model.Response{
		Content: nil, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	call := model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{}}
	result := agent.ToolResult{CallID: "call", ToolName: "tool", Contents: tool.TextContents("done"), IsError: false}
	tests := []struct {
		name  string
		event lifecycle.Event
		check func(*extensionpb.LifecycleInvocation) bool
	}{
		{
			name:  "agent start",
			event: lifecycle.Event{Agent: emptyLifecycleSource(run.EventAgentStart), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetAgentStart() != nil },
		},
		{
			name:  "agent end",
			event: lifecycle.Event{Agent: lifecycleSourceWithAgent(), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetAgentEnd() != nil },
		},
		{
			name:  "agent settled",
			event: lifecycle.Event{Agent: emptyLifecycleSource(run.EventAgentEnd), Settled: true},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetAgentSettled() != nil },
		},
		{
			name:  "turn start",
			event: lifecycle.Event{Agent: emptyLifecycleSource(run.EventTurnStart), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetTurnStart() != nil },
		},
		{
			name:  "turn end",
			event: lifecycle.Event{Agent: lifecycleSourceWithTurn(response), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetTurnEnd() != nil },
		},
		{
			name:  "message start",
			event: lifecycle.Event{Agent: emptyLifecycleSource(run.EventMessageStart), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetMessageStart() != nil },
		},
		{
			name:  "message update",
			event: lifecycle.Event{Agent: lifecycleSourceWithContent(), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetMessageUpdate() != nil },
		},
		{
			name:  "message end",
			event: lifecycle.Event{Agent: lifecycleSourceWithMessage(response), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetMessageEnd() != nil },
		},
		{
			name:  "tool execution start",
			event: lifecycle.Event{Agent: lifecycleSourceWithCall(run.EventToolExecutionStart, call), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetToolExecutionStart() != nil },
		},
		{
			name:  "tool execution update",
			event: lifecycle.Event{Agent: lifecycleSourceWithProgress(), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetToolExecutionUpdate() != nil },
		},
		{
			name:  "tool execution end",
			event: lifecycle.Event{Agent: lifecycleSourceWithResult(result), Settled: false},
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetToolExecutionEnd() != nil },
		},
	}

	// Act and assert each source event maps to its exact typed lifecycle group.
	for _, test := range tests {
		mapped, err := mapLifecycleEvent(test.event)
		require.NoError(t, err, test.name)
		assert.True(t, test.check(mapped), test.name)
	}
}

// lifecycleSourceWithAgent creates one terminal agent source.
func lifecycleSourceWithAgent() run.Event {
	event := emptyLifecycleSource(run.EventAgentEnd)
	event.Agent = mo.Some(
		run.AgentSummary{Outcome: agent.RunOutcomeCompleted, AddedHistory: nil, ErrorMessage: mo.None[string]()},
	)
	return event
}

// lifecycleSourceWithTurn creates one terminal turn source.
func lifecycleSourceWithTurn(response model.Response) run.Event {
	event := emptyLifecycleSource(run.EventTurnEnd)
	event.Turn = mo.Some(run.TurnSummary{Response: response, ToolResults: nil})
	return event
}

// lifecycleSourceWithContent creates one visible message update source.
func lifecycleSourceWithContent() run.Event {
	event := emptyLifecycleSource(run.EventTextDelta)
	event.Position = mo.Some(0)
	event.Content = mo.Some(
		model.Content{
			Kind:            model.ContentText,
			Text:            mo.Some("delta"),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	)
	return event
}

// lifecycleSourceWithMessage creates one terminal message source.
func lifecycleSourceWithMessage(response model.Response) run.Event {
	event := emptyLifecycleSource(run.EventMessageEnd)
	event.Message = mo.Some(response)
	return event
}

// lifecycleSourceWithCall creates one tool call source.
func lifecycleSourceWithCall(eventType run.EventType, call model.ToolCall) run.Event {
	event := emptyLifecycleSource(eventType)
	event.ToolCall = mo.Some(call)
	return event
}

// lifecycleSourceWithProgress creates one tool progress source.
func lifecycleSourceWithProgress() run.Event {
	event := emptyLifecycleSource(run.EventToolExecutionUpdate)
	event.ToolCall = mo.Some(model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{}})
	event.Progress = mo.Some(tool.Progress{Channel: tool.ProgressChannelStatus, Content: "working"})
	return event
}

// lifecycleSourceWithResult creates one terminal tool execution source.
func lifecycleSourceWithResult(result agent.ToolResult) run.Event {
	event := emptyLifecycleSource(run.EventToolExecutionEnd)
	event.ToolResult = mo.Some(result)
	return event
}

// TestMapLifecyclePreservesNestedProviderNeutralContent verifies turn results, tool identity, and preview fields.
func TestMapLifecyclePreservesNestedProviderNeutralContent(t *testing.T) {
	t.Parallel()

	// Arrange source payloads that carry data beyond their top-level lifecycle kind.
	response := model.Response{
		Content: nil, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	result := agent.ToolResult{CallID: "call", ToolName: "tool", Contents: tool.TextContents("done"), IsError: false}
	turn := lifecycleSourceWithTurn(response)
	turnSummary, _ := turn.Turn.Get()
	turnSummary.ToolResults = []agent.ToolResult{result}
	turn.Turn = mo.Some(turnSummary)
	progress := lifecycleSourceWithProgress()
	progress.ToolCall = mo.Some(model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{}})
	preview := emptyLifecycleSource(run.EventToolCallDelta)
	preview.Position = mo.Some(2)
	preview.Preview = mo.Some(model.ToolCallPreview{
		CallID: "call", Name: "tool", Position: 2, Provisional: true,
		Fields: []model.ToolCallPreviewField{{
			Name: "query", Kind: model.ToolCallPreviewFieldPrefix,
			Value: mo.None[any](), Prefix: mo.Some("par"),
		}},
	})

	// Act by mapping each nested source payload.
	mappedTurn, turnErr := mapLifecycleEvent(lifecycle.Event{Agent: turn, Settled: false})
	mappedProgress, progressErr := mapLifecycleEvent(lifecycle.Event{Agent: progress, Settled: false})
	mappedPreview, previewErr := mapLifecycleEvent(lifecycle.Event{Agent: preview, Settled: false})

	// Assert all provider-neutral content is retained in typed public fields.
	require.NoError(t, turnErr)
	require.Len(t, mappedTurn.GetTurnEnd().GetToolResults(), 1)
	assert.Equal(t, "call", mappedTurn.GetTurnEnd().GetToolResults()[0].GetCallId())
	require.NoError(t, progressErr)
	assert.Equal(t, "call", mappedProgress.GetToolExecutionUpdate().GetCallId())
	assert.Equal(t, "tool", mappedProgress.GetToolExecutionUpdate().GetToolName())
	require.NoError(t, previewErr)
	mappedCall := mappedPreview.GetMessageUpdate().GetToolCall()
	require.Len(t, mappedCall.GetFields(), 1)
	assert.Equal(t, "query", mappedCall.GetFields()[0].GetName())
	assert.Equal(t, "par", mappedCall.GetFields()[0].GetPrefix())
}

// TestMapLifecycleAgentEndUsesStableOutcome verifies public lifecycle data uses semantic values.
func TestMapLifecycleAgentEndUsesStableOutcome(t *testing.T) {
	t.Parallel()

	// Arrange one completed Agent Core terminal event.
	event := emptyLifecycleSource(run.EventAgentEnd)
	event.Agent = mo.Some(run.AgentSummary{
		Outcome: agent.RunOutcomeCompleted, AddedHistory: nil, ErrorMessage: mo.None[string](),
	})

	// Act by mapping it to the public lifecycle contract.
	mapped, err := mapLifecycleEvent(lifecycle.Event{Agent: event, Settled: false})

	// Assert the public outcome is stable text instead of an internal numeric value.
	require.NoError(t, err)
	assert.Equal(t, "completed", mapped.GetAgentEnd().GetOutcome())
}

// TestMapLifecycleMessageUpdateExcludesProviderContext verifies private reasoning payloads do not escape.
func TestMapLifecycleMessageUpdateExcludesProviderContext(t *testing.T) {
	t.Parallel()

	// Arrange one reasoning transition with only provider-owned replay context.
	event := emptyLifecycleSource(run.EventContentStart)
	event.Position = mo.Some(0)
	event.Content = mo.Some(model.Content{
		Kind: model.ContentReasoning, Text: mo.None[string](), Final: false,
		ProviderContext: mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: "provider", API: "private", Model: "model", CompatibilityKey: mo.None[string](),
			},
			Payload: []byte("credential-like private context"),
		}),
		ToolCall: mo.None[model.ToolCall](),
	})

	// Act by mapping the transition.
	mapped, err := mapLifecycleEvent(lifecycle.Event{Agent: event, Settled: false})

	// Assert the transition identity remains while private content is omitted.
	require.NoError(t, err)
	update := mapped.GetMessageUpdate()
	require.NotNil(t, update)
	assert.Equal(t, extensionpb.MessageUpdateKind_MESSAGE_UPDATE_KIND_CONTENT_START, update.GetKind())
	assert.Nil(t, update.GetContent())
}
