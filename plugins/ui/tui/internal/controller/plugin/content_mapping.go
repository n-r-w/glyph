package plugin

import (
	"bytes"

	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// mapContents rejects malformed blocks before they reach presentation state.
func mapContents(contents []*uiv1.ToolResultContent, allowEmpty bool) ([]presentationdomain.Content, error) {
	if len(contents) == 0 && !allowEmpty {
		return nil, errors.New("tool result contents are empty")
	}
	if contents == nil {
		return nil, nil
	}
	return lo.MapErr(
		contents,
		func(content *uiv1.ToolResultContent, index int) (presentationdomain.Content, error) {
			if content == nil {
				return presentationdomain.Content{}, fmt.Errorf("tool result content %d is missing", index)
			}
			switch content.WhichContent() {
			case uiv1.ToolResultContent_Text_case:
				return presentationdomain.Content{
					Text:      mo.Some(content.GetText()),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				}, nil
			case uiv1.ToolResultContent_Image_case:
				image := content.GetImage()
				if image == nil || image.GetMediaType() == "" || !image.HasData() {
					return presentationdomain.Content{}, fmt.Errorf("tool result image %d is invalid", index)
				}
				return presentationdomain.Content{
					MediaType: mo.Some(image.GetMediaType()),
					Data:      mo.Some(bytes.Clone(image.GetData())),
					Text:      mo.None[string](),
				}, nil
			case uiv1.ToolResultContent_Content_not_set_case:
				return presentationdomain.Content{}, fmt.Errorf("tool result content %d is missing", index)
			default:
				return presentationdomain.Content{}, fmt.Errorf("tool result content %d is invalid", index)
			}
		},
	)
}

// mapModelResponseContent rejects malformed finalized blocks before projection.
func mapModelResponseContent(content []*uiv1.ModelResponseContent) ([]presentationdomain.ModelResponseContent, error) {
	result := make([]presentationdomain.ModelResponseContent, 0, len(content))
	for index, item := range content {
		if item == nil {
			return nil, fmt.Errorf("model response content %d is missing", index)
		}
		if item.GetToolCall() != nil {
			// Final tool calls already own dedicated lifecycle lines and must not become empty model text lines.
			continue
		}
		if !item.HasKind() {
			return nil, fmt.Errorf("model response content %d kind is missing", index)
		}
		kind, err := mapModelContentKind(item.GetKind())
		if err != nil {
			return nil, fmt.Errorf("model response content %d: %w", index, err)
		}
		if !item.HasText() {
			return nil, fmt.Errorf("model response content %d text is missing", index)
		}
		result = append(result, presentationdomain.ModelResponseContent{
			Kind: kind,
			Text: mo.Some(item.GetText()),
		})
	}
	return result, nil
}

// mapModelContentDiscriminators validates both nested model-content discriminators.
func mapModelContentDiscriminators(
	lifecycleType uiv1.LifecycleType,
	contentType uiv1.ModelContentType,
	contentKind uiv1.ModelContentKind,
) (presentationdomain.ModelContentKind, error) {
	expectedType, err := expectedModelContentType(lifecycleType)
	if err != nil {
		return presentationdomain.ModelContentUnspecified, err
	}
	if contentType != expectedType {
		return presentationdomain.ModelContentUnspecified, fmt.Errorf(
			"model content type %d does not match lifecycle type %d",
			contentType, lifecycleType,
		)
	}
	return mapModelContentKind(contentKind)
}

// expectedModelContentType maps each model lifecycle boundary to its matching nested type.
func expectedModelContentType(lifecycleType uiv1.LifecycleType) (uiv1.ModelContentType, error) {
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START {
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_START, nil
	}
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA, nil
	}
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END {
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_END, nil
	}
	return uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED, fmt.Errorf(
		"lifecycle type %d does not support model content", lifecycleType,
	)
}

// mapModelContentKind converts public content identity into the TUI presentation contract.
func mapModelContentKind(kind uiv1.ModelContentKind) (presentationdomain.ModelContentKind, error) {
	switch kind {
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT:
		return presentationdomain.ModelContentText, nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
		return presentationdomain.ModelContentRefusal, nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING:
		return presentationdomain.ModelContentReasoning, nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED:
		return presentationdomain.ModelContentUnspecified, errors.New("model content kind is unspecified")
	default:
		return presentationdomain.ModelContentUnspecified, fmt.Errorf("model content kind %d is invalid", kind)
	}
}

func mapToolCallPreview(preview *uiv1.ToolCallPreview) (presentationdomain.ToolCallState, error) {
	if !preview.HasCallId() || !preview.HasName() || !preview.HasPosition() || !preview.HasProvisional() {
		return presentationdomain.ToolCallState{}, errors.New("tool call preview scalar is missing")
	}
	fields := make([]presentationdomain.ToolCallField, len(preview.GetFields()))
	for index, field := range preview.GetFields() {
		if field == nil {
			return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d is nil", index)
		}
		if !field.HasName() {
			return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d name is missing", index)
		}
		mapped := presentationdomain.ToolCallField{
			Name:   field.GetName(),
			Value:  mo.None[any](),
			Prefix: mo.None[string](),
		}
		switch field.WhichContent() {
		case uiv1.ToolCallPreviewField_Value_case:
			value := field.GetValue()
			if value == nil {
				return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d value is nil", index)
			}
			mapped.Value = mo.Some(value.AsInterface())
		case uiv1.ToolCallPreviewField_Prefix_case:
			mapped.Prefix = mo.Some(field.GetPrefix())
		case uiv1.ToolCallPreviewField_Content_not_set_case:
			return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d content is missing", index)
		}
		fields[index] = mapped
	}
	return presentationdomain.ToolCallState{
		CallID:      preview.GetCallId(),
		Name:        preview.GetName(),
		Position:    int(preview.GetPosition()),
		Provisional: preview.GetProvisional(),
		Fields:      fields,
		Arguments:   nil,
	}, nil
}

// mapProgress validates the closed progress-channel enum and assigns its output kind.
func mapProgress(event *presentationdomain.Event, channel uiv1.ProgressChannel) error {
	switch channel {
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STATUS:
		event.Kind = presentationdomain.EventToolProgress
		event.Status = mo.Some("progress")
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT:
		event.Kind = presentationdomain.EventToolOutput
		event.Stream = mo.Some(presentationdomain.OutputStdout)
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		event.Kind = presentationdomain.EventToolOutput
		event.Stream = mo.Some(presentationdomain.OutputStderr)
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED:
		return errors.New("tool progress channel is unspecified")
	default:
		return fmt.Errorf("unknown tool progress channel %d", channel)
	}
	return nil
}

// mapAvailability rejects unspecified or unknown Host availability values.
func mapAvailability(availability uiv1.Availability) (presentationdomain.Availability, error) {
	switch availability {
	case uiv1.Availability_AVAILABILITY_CHECKING_AUTHENTICATION:
		return presentationdomain.AvailabilityChecking, nil
	case uiv1.Availability_AVAILABILITY_AUTHENTICATING:
		return presentationdomain.AvailabilityAuthenticating, nil
	case uiv1.Availability_AVAILABILITY_AUTHENTICATION_FAILED:
		return presentationdomain.AvailabilityAuthenticationFailed, nil
	case uiv1.Availability_AVAILABILITY_IDLE:
		return presentationdomain.AvailabilityIdle, nil
	case uiv1.Availability_AVAILABILITY_RUNNING:
		return presentationdomain.AvailabilityRunning, nil
	case uiv1.Availability_AVAILABILITY_UNSPECIFIED:
		return presentationdomain.AvailabilityUnspecified, errors.New("availability is unspecified")
	default:
		return presentationdomain.AvailabilityUnspecified, fmt.Errorf("unknown availability %d", availability)
	}
}
