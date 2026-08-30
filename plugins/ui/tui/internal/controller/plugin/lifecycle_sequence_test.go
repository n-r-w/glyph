package plugin

import (
	"encoding/json/v2"

	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestSemanticLifecycleSequenceUsesContractMapping verifies shared lifecycle data through the standard consumer mapping.
func TestSemanticLifecycleSequenceUsesContractMapping(t *testing.T) {
	t.Parallel()
	// Arrange the semantic lifecycle fixture and an initialized presentation state.
	payload, err := os.ReadFile(filepath.Join(repositoryRoot(t), "testdata", "semantic-ui-lifecycle.json"))
	require.NoError(t, err)
	var sequence []semanticFrame
	require.NoError(t, json.Unmarshal(payload, &sequence))

	service := presentationusecase.New()
	initial, err := mapRequest(initializationRequest())
	require.NoError(t, err)
	// Act by mapping and applying every fixture frame.
	state := service.Apply(presentationdomain.State{}, initial)
	for _, frame := range sequence {
		request := lifecycleRequest(frame)
		event, mapErr := mapRequest(request)
		require.NoError(t, mapErr)
		state = service.Apply(state, event)
	}

	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)
	// Assert the final state contains the expected model and tool transcript entries.
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("Request complete."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	})
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("bash"),
		Status:   mo.Some("completed"),
		Text:     mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	})
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("bash"),
		Text:     mo.Some("tool-ok\n\n[Exit code: 0]\n"),
		Contents: mo.Some([]presentationdomain.Content{{
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
	Type               string `json:"type"`
	ToolName           string `json:"tool_name"`
	ToolStatus         string `json:"tool_status"`
	Text               string `json:"text"`
	ToolResultContents []struct {
		Text string `json:"text"`
	} `json:"tool_result_contents"`
	ModelText    string `json:"model_text"`
	Outcome      string `json:"outcome"`
	Availability string `json:"availability"`
}

// lifecycleRequest builds a public protobuf frame for the real controller mapper.
func lifecycleRequest(frame semanticFrame) *uiv1.OpenRequest {
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
	case "agent_settled":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED
	case "agent_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END
	case "availability":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED
	}
	lifecycle := uiv1.LifecycleEvent_builder{
		Type:               new(typeValue),
		ToolName:           nil,
		Text:               nil,
		Outcome:            nil,
		RunId:              new("run"),
		ToolCallId:         nil,
		ProgressChannel:    nil,
		IsError:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build()
	if frame.Type == "message_end" {
		var content []*uiv1.ModelResponseContent
		if frame.ModelText != "" {
			content = []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				Text: new(frame.ModelText), ToolCall: nil,
			}.Build()}
		}
		lifecycle.SetModelResponse(uiv1.ModelResponse_builder{
			Content:       content,
			Text:          nil,
			Outcome:       nil,
			ErrorMessage:  nil,
			Provider:      nil,
			Model:         nil,
			ResponseId:    nil,
			Usage:         nil,
			Diagnostics:   nil,
			ResponseModel: nil,
		}.Build())
	}
	if frame.Type == "tool_execution_start" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
	}
	if frame.Type == "tool_result" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
		contents := make([]*uiv1.ToolResultContent, 0, len(frame.ToolResultContents))
		for _, content := range frame.ToolResultContents {
			//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Text field.
			contents = append(contents, uiv1.ToolResultContent_builder{
				Text: new(content.Text),
			}.Build())
		}
		lifecycle.SetToolResultContents(contents)
	}
	if frame.Type == "tool_execution_end" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
		lifecycle.SetIsError(frame.ToolStatus != "ok")
	}
	if frame.Type == "tool_result" {
		lifecycle.SetIsError(false)
	}
	if frame.Type == "agent_end" {
		lifecycle.SetOutcome(frame.Outcome)
	}
	if frame.Type == "availability" {
		lifecycle.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
	}
	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	return uiv1.OpenRequest_builder{
		Lifecycle:          lifecycle,
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil, SessionTree: nil, SessionTreeNavigation: nil, SessionTreeFailed: nil,
	}.Build()
}

// repositoryRoot resolves shared testdata from the source file location.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", ".."))
}

// TestMapLifecyclePreservesRefusalKind verifies refusal deltas stay distinct from ordinary model text.
func TestMapLifecyclePreservesRefusalKind(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL),
			Position: new(int32(3)),
			Text:     new("cannot help"),
		}.Build(),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventModelDelta, event.Kind)
	assert.Equal(t, mo.Some(presentationdomain.ModelContentRefusal), event.ModelContentKind)
	assert.Equal(t, mo.Some("cannot help"), event.Text)
}

// TestMapLifecyclePreservesFinalizedVisibleBlocks verifies mixed visible content reaches presentation state.
func TestMapLifecyclePreservesFinalizedVisibleBlocks(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
		ModelResponse: uiv1.ModelResponse_builder{
			Content: []*uiv1.ModelResponseContent{
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING),
					Text: new("hidden"), ToolCall: nil,
				}.Build(),
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
					Text: new("answer"), ToolCall: nil,
				}.Build(),
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL),
					Text: new("cannot help"), ToolCall: nil,
				}.Build(),
			},
			Text:          nil,
			Outcome:       nil,
			ErrorMessage:  nil,
			Provider:      nil,
			Model:         nil,
			ResponseId:    nil,
			Usage:         nil,
			Diagnostics:   nil,
			ResponseModel: nil,
		}.Build(),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.ModelResponseContent{
		{
			Kind: presentationdomain.ModelContentReasoning,
			Text: mo.Some("hidden"),
		},
		{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("answer"),
		},
		{
			Kind: presentationdomain.ModelContentRefusal,
			Text: mo.Some("cannot help"),
		},
	}, event.ModelResponseContent)
}
