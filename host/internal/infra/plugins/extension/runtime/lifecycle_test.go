//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// emptyLifecycleSource creates one source event with every optional payload absent.
func emptyLifecycleSource(eventType agent.EventType) extensionruntime.LifecycleInvocation {
	return extensionruntime.LifecycleInvocation{
		Context:      extension.Context{},
		Settled:      false,
		TurnResults:  nil,
		Outcome:      mo.None[agent.RunOutcome](),
		ErrorMessage: mo.None[string](),
		Type:         eventType,
		RunID:        "run",
		Position:     mo.None[int](),
		Content:      mo.None[extensionruntime.Content](),
		Response:     mo.None[extensionruntime.Response](),
		Preview:      mo.None[model.ToolCallPreview](),
		ToolCall:     mo.None[model.ToolCall](),
		Progress:     mo.None[tool.Progress](),
		ToolResult:   mo.None[agent.ToolResult](),
	}
}

// TestMapLifecycleEventCoversEveryObserverPayload verifies each approved group has one typed public variant.
func TestMapLifecycleEventCoversEveryObserverPayload(t *testing.T) {
	t.Parallel()

	// Arrange one valid source event for each Agent Core lifecycle group.
	response := extensionruntime.Response{
		Content: nil, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	call := model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{}}
	result := agent.ToolResult{CallID: "call", ToolName: "tool", Contents: tool.TextContents("done"), IsError: false}
	tests := []struct {
		name  string
		event extensionruntime.LifecycleInvocation
		check func(*extensionpb.LifecycleInvocation) bool
	}{
		{
			name:  "agent start",
			event: emptyLifecycleSource(agent.EventAgentStart),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetAgentStart() != nil },
		},
		{
			name:  "agent end",
			event: lifecycleSourceWithAgent(),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetAgentEnd() != nil },
		},
		{
			name:  "agent settled",
			event: settledLifecycleSource(),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetAgentSettled() != nil },
		},
		{
			name:  "turn start",
			event: emptyLifecycleSource(agent.EventTurnStart),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetTurnStart() != nil },
		},
		{
			name:  "turn end",
			event: lifecycleSourceWithTurn(response),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetTurnEnd() != nil },
		},
		{
			name:  "message start",
			event: emptyLifecycleSource(agent.EventMessageStart),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetMessageStart() != nil },
		},
		{
			name:  "message update",
			event: lifecycleSourceWithContent(),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetMessageUpdate() != nil },
		},
		{
			name:  "message end",
			event: lifecycleSourceWithMessage(response),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetMessageEnd() != nil },
		},
		{
			name:  "tool execution start",
			event: lifecycleSourceWithCall(agent.EventToolExecutionStart, call),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetToolExecutionStart() != nil },
		},
		{
			name:  "tool execution update",
			event: lifecycleSourceWithProgress(),
			check: func(value *extensionpb.LifecycleInvocation) bool { return value.GetToolExecutionUpdate() != nil },
		},
		{
			name:  "tool execution end",
			event: lifecycleSourceWithResult(result),
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
func lifecycleSourceWithAgent() extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventAgentEnd)
	event.Outcome = mo.Some(agent.RunOutcomeCompleted)
	return event
}

// lifecycleSourceWithTurn creates one terminal turn source.
func lifecycleSourceWithTurn(response extensionruntime.Response) extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventTurnEnd)
	event.Response = mo.Some(response)
	return event
}

// lifecycleSourceWithContent creates one visible message update source.
func lifecycleSourceWithContent() extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventTextDelta)
	event.Position = mo.Some(0)
	event.Content = mo.Some(
		extensionruntime.Content{
			Kind:     model.ContentText,
			Text:     mo.Some("delta"),
			ToolCall: mo.None[model.ToolCall](),
		},
	)
	return event
}

// lifecycleSourceWithMessage creates one terminal message source.
func lifecycleSourceWithMessage(response extensionruntime.Response) extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventMessageEnd)
	event.Response = mo.Some(response)
	return event
}

// lifecycleSourceWithCall creates one tool call source.
func lifecycleSourceWithCall(eventType agent.EventType, call model.ToolCall) extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(eventType)
	event.ToolCall = mo.Some(call)
	return event
}

// lifecycleSourceWithProgress creates one tool progress source.
func lifecycleSourceWithProgress() extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventToolExecutionUpdate)
	event.ToolCall = mo.Some(model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{}})
	event.Progress = mo.Some(tool.Progress{Channel: tool.ProgressChannelStatus, Content: "working"})
	return event
}

// lifecycleSourceWithResult creates one terminal tool execution source.
func lifecycleSourceWithResult(result agent.ToolResult) extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventToolExecutionEnd)
	event.ToolResult = mo.Some(result)
	return event
}

// TestMapLifecyclePreservesNestedProviderNeutralContent verifies turn results, tool identity, and preview fields.
func TestMapLifecyclePreservesNestedProviderNeutralContent(t *testing.T) {
	t.Parallel()

	// Arrange source payloads that carry data beyond their top-level lifecycle kind.
	response := extensionruntime.Response{
		Content: nil, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	result := agent.ToolResult{CallID: "call", ToolName: "tool", Contents: tool.TextContents("done"), IsError: false}
	turn := lifecycleSourceWithTurn(response)
	turn.TurnResults = []agent.ToolResult{result}
	progress := lifecycleSourceWithProgress()
	progress.ToolCall = mo.Some(model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{}})
	preview := emptyLifecycleSource(agent.EventToolCallDelta)
	preview.Position = mo.Some(2)
	preview.Preview = mo.Some(model.ToolCallPreview{
		CallID: "call", Name: "tool", Position: 2, Provisional: true,
		Fields: []model.ToolCallPreviewField{{
			Name: "query", Kind: model.ToolCallPreviewFieldPrefix,
			Value: mo.None[any](), Prefix: mo.Some("par"),
		}},
	})

	// Act by mapping each nested source payload.
	mappedTurn, turnErr := mapLifecycleEvent(turn)
	mappedProgress, progressErr := mapLifecycleEvent(progress)
	mappedPreview, previewErr := mapLifecycleEvent(preview)

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
	event := emptyLifecycleSource(agent.EventAgentEnd)
	event.Outcome = mo.Some(agent.RunOutcomeCompleted)

	// Act by mapping it to the public lifecycle contract.
	mapped, err := mapLifecycleEvent(event)

	// Assert the public outcome is stable text instead of an internal numeric value.
	require.NoError(t, err)
	assert.Equal(t, "completed", mapped.GetAgentEnd().GetOutcome())
}

// settledLifecycleSource creates a Host settlement notification without an agent payload.
func settledLifecycleSource() extensionruntime.LifecycleInvocation {
	event := emptyLifecycleSource(agent.EventAgentEnd)
	event.Settled = true
	return event
}
