//go:build integration

package presentation

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestSemanticLifecycleSequenceUsesContractMapping verifies shared lifecycle data through the standard consumer
// mapping.
func TestSemanticLifecycleSequenceUsesContractMapping(t *testing.T) {
	t.Parallel()
	// Arrange the semantic lifecycle fixture and an initialized presentation state.
	payload, err := os.ReadFile(filepath.Join(repositoryRoot(t), "testdata", "semantic-ui-lifecycle.json"))
	require.NoError(t, err)
	var sequence []semanticFrame
	require.NoError(t, json.Unmarshal(payload, &sequence))
	service := newProjectionService(t)
	// Act by decoding the real fixture and applying it at the application owner.
	for _, frame := range sequence {
		event, mapErr := semanticEvent(frame)
		require.NoError(t, mapErr)
		require.NoError(t, service.applyInput(mo.Some(event)))
	}

	state := service.model.state
	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, mo.Some(AvailabilityIdle), state.Availability)
	// Assert the final state contains the expected model and tool transcript entries.
	assert.Contains(t, state.Transcript, Line{
		Kind:     LineModel,
		Text:     mo.Some("Request complete."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]Content](),
	})
	assert.Contains(t, state.Transcript, Line{
		Kind:     LineToolDone,
		ToolName: mo.Some("bash"),
		Status:   mo.Some("completed"),
		Text:     mo.None[string](),
		Contents: mo.None[[]Content](),
	})
	assert.Contains(t, state.Transcript, Line{
		Kind:     LineToolDone,
		ToolName: mo.Some("bash"),
		Text:     mo.Some("tool-ok\n\n[Exit code: 0]\n"),
		Contents: mo.Some([]Content{{
			Text:      mo.Some("tool-ok\n\n[Exit code: 0]\n"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		Status: mo.None[string](),
	})
	assert.Empty(t, state.ActiveTools)
}

// semanticFrame describes the stable lifecycle fields shared by both fixtures.
type semanticFrame struct {
	Type               string                      `json:"type"`
	ToolName           string                      `json:"tool_name"`
	ToolStatus         string                      `json:"tool_status"`
	Text               string                      `json:"text"`
	ToolResultContents []semanticToolResultContent `json:"tool_result_contents"`
	ModelText          string                      `json:"model_text"`
	Outcome            string                      `json:"outcome"`
	Availability       string                      `json:"availability"`
}

// semanticToolResultContent describes one fixture tool-result content item.
type semanticToolResultContent struct {
	// Text contains one fixture text result.
	Text string `json:"text"`
}

// semanticEvent maps one semantic fixture through its operation-stream owner.
func semanticEvent(frame semanticFrame) (plugininput.Payload, error) {
	if frame.Type == "agent_settled" {
		completed := new(uiv1.HostCompleted)
		completed.SetSubmit(new(uiv1.SubmitCompleted))
		event, _, err := plugininput.DecodeCompleted(completed)
		return event, err
	}
	if frame.Type == "availability" {
		connection := new(uiv1.HostConnectionEvent)
		connection.SetAvailabilityChanged(uiv1.AvailabilityChanged_builder{
			Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
		}.Build())
		return plugininput.DecodeConnectionEvent(connection)
	}
	update, err := plugininput.DecodeLifecycle(semanticLifecycle(frame))
	return plugininput.AgentPayload(update), err
}

// semanticLifecycle builds one retained agent lifecycle payload.
func semanticLifecycle(frame semanticFrame) *uiv1.AgentEvent {
	typeValue := uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED
	switch frame.Type {
	case "agent_start":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START
	case "message_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END
	case "tool_execution_start":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START
	case "tool_execution_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END
	case "tool_result":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT
	case "agent_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END
	}
	lifecycle := uiv1.AgentEvent_builder{
		Type: new(typeValue), ToolName: nil, Text: nil, Outcome: nil, RunId: new("run"),
		ToolCallId: nil, ProgressChannel: nil, IsError: nil, ErrorMessage: nil, Availability: nil,
		ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
		ToolResultContents: nil,
	}.Build()
	if frame.Type == "message_end" {
		var content []*uiv1.ModelResponseContent
		if frame.ModelText != "" {
			content = []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: new(frame.ModelText), ToolCall: nil,
			}.Build()}
		}
		lifecycle.SetModelResponse(uiv1.ModelResponse_builder{
			Content: content, Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
			ResponseId: nil, Usage: nil, Diagnostics: nil, ResponseModel: nil,
		}.Build())
	}
	if frame.Type == "tool_execution_start" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
	}
	if frame.Type == "tool_result" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
		lifecycle.SetToolResultContents(
			lo.Map(frame.ToolResultContents, func(content semanticToolResultContent, _ int) *uiv1.ToolResultContent {
				return uiv1.ToolResultContent_builder{Text: new(content.Text), Image: nil}.Build()
			}),
		)
		lifecycle.SetIsError(false)
	}
	if frame.Type == "tool_execution_end" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
		lifecycle.SetIsError(frame.ToolStatus != "ok")
	}
	if frame.Type == "agent_end" {
		lifecycle.SetOutcome(frame.Outcome)
	}
	return lifecycle
}

// repositoryRoot resolves shared testdata from the source file location.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", ".."))
}
