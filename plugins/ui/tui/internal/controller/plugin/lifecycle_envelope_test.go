//go:build !integration

package plugin

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapLifecycleRejectsInactiveAgentStartResponse verifies stale lifecycle payloads fail at ingress.
func TestMapLifecycleRejectsInactiveAgentStartResponse(t *testing.T) {
	t.Parallel()

	lifecycle := messageEndLifecycle(t, nil)
	lifecycle.SetType(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START)
	_, err := mapLifecycle(lifecycle)

	require.Error(t, err)
}

// TestMapLifecycleValidatesActiveAndInactiveFieldsForEveryType verifies the complete lifecycle shape table.
func TestMapLifecycleValidatesActiveAndInactiveFieldsForEveryType(t *testing.T) {
	t.Parallel()

	validLifecycle := func(lifecycleType uiv1.LifecycleType) *uiv1.LifecycleEvent {
		lifecycle := uiv1.LifecycleEvent_builder{
			Type: new(lifecycleType), RunId: new("run"), Text: nil, ToolCallId: nil, ToolName: nil,
			ProgressChannel: nil, IsError: nil, Outcome: nil, ErrorMessage: nil, Availability: nil,
			ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
			ToolResultContents: nil,
		}.Build()
		switch lifecycleType {
		case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
			nestedType := uiv1.ModelContentType_MODEL_CONTENT_TYPE_START
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
				nestedType = uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA
			}
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END {
				nestedType = uiv1.ModelContentType_MODEL_CONTENT_TYPE_END
			}
			text := (*string)(nil)
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
				text = new("")
			}
			lifecycle.SetModelContent(uiv1.ModelContent_builder{
				Type: new(nestedType), Position: new(int32(0)), Text: text,
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
			lifecycle.SetModelResponse(uiv1.ModelResponse_builder{
				Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
				ResponseId: nil, Usage: nil, Diagnostics: nil, Content: nil, ResponseModel: nil,
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA:
			lifecycle.SetToolCallPreview(uiv1.ToolCallPreview_builder{
				CallId: new("call"), Name: new("tool"), Position: new(int32(0)), Provisional: new(true), Fields: nil,
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
			lifecycle.SetFinalToolCall(uiv1.FinalToolCall_builder{
				CallId: new("call"), Name: new("tool"), Position: new(int32(0)),
				Arguments: &structpb.Struct{Fields: map[string]*structpb.Value{}},
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
			lifecycle.SetToolCallId("")
			lifecycle.SetToolName("")
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE:
			lifecycle.SetText("")
			lifecycle.SetProgressChannel(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT)
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
			lifecycle.SetToolCallId("")
			lifecycle.SetToolName("")
			lifecycle.SetIsError(false)
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
			lifecycle.SetToolCallId("")
			lifecycle.SetToolName("")
			lifecycle.SetIsError(false)
			lifecycle.SetToolResultContents([]*uiv1.ToolResultContent{uiv1.ToolResultContent_builder{
				Text: new(""), Image: nil,
			}.Build()})
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END:
			lifecycle.SetText("")
		case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
			lifecycle.SetOutcome("")
		case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
			lifecycle.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
		case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
			uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		}
		return lifecycle
	}

	lifecycleTypes := []uiv1.LifecycleType{
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END,
	}
	for _, lifecycleType := range lifecycleTypes {
		t.Run(lifecycleType.String(), func(t *testing.T) {
			t.Parallel()
			valid := roundTripLifecycle(t, validLifecycle(lifecycleType))
			_, err := mapLifecycle(valid)
			require.NoError(t, err)

			malformed := roundTripLifecycle(t, validLifecycle(lifecycleType))
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
				malformed.SetModelResponse(uiv1.ModelResponse_builder{
					Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
					ResponseId: nil, Usage: nil, Diagnostics: nil, Content: nil, ResponseModel: nil,
				}.Build())
			} else {
				malformed.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
			}
			_, err = mapLifecycle(malformed)
			require.Error(t, err)
		})
	}
}

func TestMapLifecycleRejectsMissingSelectedModelAndPreviewPayloads(t *testing.T) {
	t.Parallel()

	lifecycle := func(
		lifecycleType uiv1.LifecycleType,
		modelContent *uiv1.ModelContent,
		preview *uiv1.ToolCallPreview,
	) *uiv1.LifecycleEvent {
		return uiv1.LifecycleEvent_builder{
			Type:               new(lifecycleType),
			RunId:              new("run"),
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelContent:       modelContent,
			ModelResponse:      nil,
			ToolCallPreview:    preview,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build()
	}
	nilFieldPreview := uiv1.ToolCallPreview_builder{
		CallId:      nil,
		Name:        nil,
		Position:    nil,
		Provisional: nil,
		Fields:      []*uiv1.ToolCallPreviewField{nil},
	}.Build()
	unsetFieldPreview := uiv1.ToolCallPreview_builder{
		CallId:      nil,
		Name:        nil,
		Position:    nil,
		Provisional: nil,
		Fields: []*uiv1.ToolCallPreviewField{uiv1.ToolCallPreviewField_builder{
			Name:   new("path"),
			Value:  nil,
			Prefix: nil,
		}.Build()},
	}.Build()

	testCases := []struct {
		name      string
		lifecycle *uiv1.LifecycleEvent
	}{
		{
			name:      "message end response",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END, nil, nil),
		},
		{
			name:      "nil preview field",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START, nil, nilFieldPreview),
		},
		{
			name:      "preview field content",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA, nil, unsetFieldPreview),
		},
		{
			name: "model position",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START, uiv1.ModelContent_builder{
				Type:     nil,
				Position: nil,
				Text:     nil,
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build(), nil),
		},
		{
			name: "model kind",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END, uiv1.ModelContent_builder{
				Type:     nil,
				Position: new(int32(0)),
				Text:     nil,
				Kind:     nil,
			}.Build(), nil),
		},
		{
			name: "text delta text",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA, uiv1.ModelContent_builder{
				Type:     nil,
				Position: new(int32(0)),
				Text:     nil,
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build(), nil),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(testCase.lifecycle)
			require.Error(t, err)
		})
	}
}

// TestMapLifecycleRejectsMissingRequiredScalarFields verifies each lifecycle variant checks its scalar contract.
func TestMapLifecycleRejectsMissingRequiredScalarFields(t *testing.T) {
	t.Parallel()

	lifecycle := func(lifecycleType uiv1.LifecycleType) *uiv1.LifecycleEvent {
		return uiv1.LifecycleEvent_builder{
			Type: new(lifecycleType), RunId: new("run"), Text: nil, ToolCallId: nil, ToolName: nil,
			ProgressChannel: nil, IsError: nil, Outcome: nil, ErrorMessage: nil, Availability: nil,
			ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
			ToolResultContents: nil,
		}.Build()
	}
	missingRunID := uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: nil, Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil, ModelResponse: nil,
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build()
	missingModelType := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START)
	missingModelType.SetModelContent(uiv1.ModelContent_builder{
		Type: nil, Position: new(int32(0)), Text: nil,
		Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
	}.Build())
	missingPreviewProvisional := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START)
	missingPreviewProvisional.SetToolCallPreview(uiv1.ToolCallPreview_builder{
		CallId: new("call"), Name: new("tool"), Position: new(int32(0)), Provisional: nil, Fields: nil,
	}.Build())
	missingFinalPosition := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END)
	missingFinalPosition.SetFinalToolCall(uiv1.FinalToolCall_builder{
		CallId: new("call"), Name: new("tool"), Position: nil,
		Arguments: &structpb.Struct{Fields: map[string]*structpb.Value{}},
	}.Build())
	missingStartCallID := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START)
	missingStartCallID.SetToolName("tool")
	missingProgressText := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE)
	missingProgressText.SetProgressChannel(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT)
	missingEndCallID := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END)
	missingEndCallID.SetToolName("tool")
	missingEndCallID.SetIsError(false)

	tests := []struct {
		name      string
		lifecycle *uiv1.LifecycleEvent
	}{
		{name: "run ID", lifecycle: missingRunID},
		{name: "model content type", lifecycle: missingModelType},
		{name: "tool call preview provisional", lifecycle: missingPreviewProvisional},
		{name: "final tool call position", lifecycle: missingFinalPosition},
		{name: "tool execution start call ID", lifecycle: missingStartCallID},
		{name: "tool progress text", lifecycle: missingProgressText},
		{name: "tool execution end call ID", lifecycle: missingEndCallID},
		{name: "turn end text", lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END)},
		{name: "agent end outcome", lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(roundTripLifecycle(t, test.lifecycle))
			require.Error(t, err)
		})
	}
}
