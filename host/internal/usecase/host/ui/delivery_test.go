package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDeliveryReportsRuntimeFailure sends one safe identity-bearing error frame.
func TestDeliveryReportsRuntimeFailure(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	channel.EXPECT().Send(domainui.Frame{
		Kind: domainui.FrameError,
		Initialization: domainui.Initialization{
			SelectedUIID: "", StartupContent: nil, Extensions: nil, Availability: 0,
		},
		Lifecycle: domainui.Lifecycle{
			Type: 0, RunID: "", Position: 0, Text: "", ToolCallID: "", ToolName: "",
			ProgressChannel: 0, IsError: false, Outcome: "", ErrorMessage: "", Availability: 0,
		},
		AuthorizationURL:    "",
		Text:                "extension crashed-plugin unavailable: extension process exited",
		RetryAuthentication: false,
	})

	err := NewDelivery(channel).ReportRuntimeFailure(t.Context(), tool.RuntimeFailure{
		PluginID: "crashed-plugin", Condition: tool.RuntimeUnavailableProcessExited,
	})

	require.NoError(t, err)
}

// TestDeliveryFiltersProviderContextFromMessageEnd verifies opaque provider data cannot cross the UI boundary.
func TestDeliveryFiltersProviderContextFromMessageEnd(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	var delivered domainui.Frame
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		delivered = frame
		return nil
	})
	event := run.Event{
		Type: run.EventMessageEnd, RunID: "run-1", Position: 0, Delta: "",
		Message: agent.ModelResponse{
			Items: []agent.ModelItem{
				{
					Kind: agent.ModelItemProviderContext, Text: "",
					ProviderContext: agent.ProviderContext{ProviderID: "secret-provider", Payload: []byte("encrypted-secret")},
					ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
				},
				{
					Kind: agent.ModelItemText, Text: "visible text",
					ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
					ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
				},
			},
			Outcome: agent.ModelOutcomeStop, ErrorMessage: "",
		},
		ToolCall:   agent.ToolCall{ID: "", Name: "", Arguments: nil},
		Progress:   tool.Progress{Channel: 0, Content: ""},
		ToolResult: agent.ToolResult{CallID: "", ToolName: "", Content: "", IsError: false},
		Turn: run.TurnSummary{
			Response: agent.ModelResponse{Items: nil, Outcome: 0, ErrorMessage: ""}, ToolResults: nil,
		},
		Agent: run.AgentSummary{Outcome: 0, AddedHistory: nil, ErrorMessage: ""},
	}

	err := NewDelivery(channel).DeliverAgent(t.Context(), event)

	require.NoError(t, err)
	assert.Equal(t, domainui.FrameLifecycle, delivered.Kind)
	assert.Equal(t, "visible text", delivered.Lifecycle.Text)
	assert.NotContains(t, delivered.Lifecycle.Text, "encrypted-secret")
	assert.Equal(t, "stop", delivered.Lifecycle.Outcome)
}

// TestDeliveryPreservesAgentThenSettlementOrder verifies Host settlement remains a separate final lifecycle item.
func TestDeliveryPreservesAgentThenSettlementOrder(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	frames := make([]domainui.Frame, 0, 2)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		frames = append(frames, frame)
		return nil
	}).Times(2)
	delivery := NewDelivery(channel)

	require.NoError(t, delivery.DeliverAgent(t.Context(), run.Event{
		Type: run.EventAgentEnd, RunID: "run-1", Position: 0, Delta: "",
		Message:    agent.ModelResponse{Items: nil, Outcome: 0, ErrorMessage: ""},
		ToolCall:   agent.ToolCall{ID: "", Name: "", Arguments: nil},
		Progress:   tool.Progress{Channel: 0, Content: ""},
		ToolResult: agent.ToolResult{CallID: "", ToolName: "", Content: "", IsError: false},
		Turn:       run.TurnSummary{Response: agent.ModelResponse{Items: nil, Outcome: 0, ErrorMessage: ""}, ToolResults: nil},
		Agent:      run.AgentSummary{Outcome: agent.RunOutcomeCompleted, AddedHistory: nil, ErrorMessage: ""},
	}))
	require.NoError(t, delivery.DeliverSettled(t.Context(), "run-1"))

	require.Len(t, frames, 2)
	assert.Equal(t, domainui.LifecycleAgentEnd, frames[0].Lifecycle.Type)
	assert.Equal(t, domainui.LifecycleAgentSettled, frames[1].Lifecycle.Type)
}
