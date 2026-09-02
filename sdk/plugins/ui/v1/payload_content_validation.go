package uiv1

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// expectedModelContentType returns the model transition required by one lifecycle kind.
func expectedModelContentType(lifecycleType uiv1.LifecycleType) (uiv1.ModelContentType, error) {
	switch lifecycleType {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_START, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_END, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START, uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START, uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END, uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED,
			fmt.Errorf("Host lifecycle type %d does not support model content", lifecycleType)
	default:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED,
			fmt.Errorf("Host lifecycle type %d is unknown", lifecycleType)
	}
}

// validateModelContentKind rejects unspecified and unknown model content kinds.
func validateModelContentKind(kind uiv1.ModelContentKind) error {
	switch kind {
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
		uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING,
		uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
		return nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED:
		return errors.New("Host model content kind is required")
	default:
		return fmt.Errorf("Host model content kind %d is unknown", kind)
	}
}

// validateModelResponse validates nested finalized response content.
func validateModelResponse(response *uiv1.ModelResponse) error {
	if response == nil {
		return errors.New("Host model response is required")
	}
	for index, content := range response.GetContent() {
		if content == nil {
			return fmt.Errorf("Host model response content %d is required", index)
		}
		if call := content.GetToolCall(); call != nil {
			if err := validateFinalToolCall(call); err != nil {
				return fmt.Errorf("Host model response content %d: %w", index, err)
			}
			continue
		}
		if !content.HasKind() {
			return fmt.Errorf("Host model response content %d kind is required", index)
		}
		if err := validateModelContentKind(content.GetKind()); err != nil {
			return fmt.Errorf("Host model response content %d: %w", index, err)
		}
		if !content.HasText() {
			return fmt.Errorf("Host model response content %d text is required", index)
		}
	}
	return nil
}

// validateToolCallPreview validates preview identity and decoded fields.
func validateToolCallPreview(preview *uiv1.ToolCallPreview) error {
	if preview == nil || !preview.HasCallId() || !preview.HasName() ||
		!preview.HasPosition() || !preview.HasProvisional() {
		return errors.New("Host tool call preview fields are required")
	}
	for index, field := range preview.GetFields() {
		if field == nil {
			return fmt.Errorf("Host tool call preview field %d is required", index)
		}
		if !field.HasName() {
			return fmt.Errorf("Host tool call preview field %d name is required", index)
		}
		switch field.WhichContent() {
		case uiv1.ToolCallPreviewField_Value_case:
			if field.GetValue() == nil {
				return fmt.Errorf("Host tool call preview field %d value is required", index)
			}
		case uiv1.ToolCallPreviewField_Prefix_case:
			continue
		case uiv1.ToolCallPreviewField_Content_not_set_case:
			return fmt.Errorf("Host tool call preview field %d content is required", index)
		default:
			return fmt.Errorf("Host tool call preview field %d content is unknown", index)
		}
	}
	return nil
}

// validateFinalToolCall validates one finalized tool call.
func validateFinalToolCall(call *uiv1.FinalToolCall) error {
	if call == nil || !call.HasCallId() || !call.HasName() || !call.HasPosition() || call.GetArguments() == nil {
		return errors.New("Host final tool call fields are required")
	}
	return nil
}

// validateProgressChannel rejects unspecified and unknown progress channels.
func validateProgressChannel(channel uiv1.ProgressChannel) error {
	switch channel {
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STATUS,
		uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT,
		uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		return nil
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED:
		return errors.New("Host tool progress channel is required")
	default:
		return fmt.Errorf("Host tool progress channel %d is unknown", channel)
	}
}

// validateToolResultContents validates typed tool result blocks.
func validateToolResultContents(contents []*uiv1.ToolResultContent, allowEmpty bool) error {
	if len(contents) == 0 && !allowEmpty {
		return errors.New("Host tool result contents are required")
	}
	for index, content := range contents {
		if content == nil {
			return fmt.Errorf("Host tool result content %d is required", index)
		}
		switch content.WhichContent() {
		case uiv1.ToolResultContent_Text_case:
			continue
		case uiv1.ToolResultContent_Image_case:
			image := content.GetImage()
			if image == nil || !image.HasMediaType() || image.GetMediaType() == "" || !image.HasData() {
				return fmt.Errorf("Host tool result image %d fields are required", index)
			}
		case uiv1.ToolResultContent_Content_not_set_case:
			return fmt.Errorf("Host tool result content %d payload is required", index)
		default:
			return fmt.Errorf("Host tool result content %d payload is unknown", index)
		}
	}
	return nil
}
