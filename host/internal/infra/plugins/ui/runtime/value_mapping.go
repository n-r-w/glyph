package runtime

import (
	"bytes"
	"errors"
	"fmt"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/structpb"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/tool"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapLifecycleType converts Host lifecycle identity to the public contract.
//
//nolint:gocyclo // The flat switch maps the complete lifecycle enum.
func mapLifecycleType(value controllerui.LifecycleType) uiv1.LifecycleType {
	switch value {
	case controllerui.LifecycleAgentStart:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START
	case controllerui.LifecycleTurnStart:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START
	case controllerui.LifecycleMessageStart:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START
	case controllerui.LifecycleModelContentStart:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START
	case controllerui.LifecycleModelTextDelta:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA
	case controllerui.LifecycleModelContentEnd:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END
	case controllerui.LifecycleToolCallStart:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START
	case controllerui.LifecycleToolCallDelta:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA
	case controllerui.LifecycleToolCallEnd:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END
	case controllerui.LifecycleMessageEnd:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END
	case controllerui.LifecycleToolExecutionStart:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START
	case controllerui.LifecycleToolExecutionUpdate:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE
	case controllerui.LifecycleToolExecutionEnd:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END
	case controllerui.LifecycleToolResult:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT
	case controllerui.LifecycleTurnEnd:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END
	case controllerui.LifecycleAgentEnd:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END
	default:
		return uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED
	}
}

// mapToolResultContents copies ordered domain blocks into the public UI contract.
func mapToolResultContents(contents []tool.ResultContent) []*uiv1.ToolResultContent {
	return lo.FilterMap(contents, func(content tool.ResultContent, _ int) (*uiv1.ToolResultContent, bool) {
		switch content.Kind {
		case tool.ResultContentText:
			text, present := content.Text.Get()
			if !present {
				return nil, false
			}
			//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Text field.
			return uiv1.ToolResultContent_builder{
				Text: &text,
			}.Build(), true
		case tool.ResultContentImage:
			image, present := content.Image.Get()
			if !present {
				return nil, false
			}
			mappedImage := uiv1.ToolResultImage_builder{
				MediaType: &image.MediaType,
				Data:      nil,
			}.Build()
			mappedImage.SetData(bytes.Clone(image.Data))
			//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Image field.
			return uiv1.ToolResultContent_builder{
				Image: mappedImage,
			}.Build(), true
		}
		return nil, false
	})
}

func mapToolCallPreview(preview controllerui.ToolCallPreview) (*uiv1.ToolCallPreview, error) {
	fields := make([]*uiv1.ToolCallPreviewField, 0, len(preview.Fields))
	for _, field := range preview.Fields {
		mapped := uiv1.ToolCallPreviewField_builder{
			Name:   new(field.Name),
			Value:  nil,
			Prefix: nil,
		}.Build()
		if field.Complete {
			value, present := field.Value.Get()
			if !present {
				return nil, errors.New("map UI tool call preview: complete value is required")
			}
			protobufValue, err := structpb.NewValue(value)
			if err != nil {
				return nil, fmt.Errorf("map UI tool call preview value: %w", err)
			}
			mapped.SetValue(proto.ValueOrDefault(protobufValue))
		} else {
			prefix, present := field.Prefix.Get()
			if !present {
				return nil, errors.New("map UI tool call preview: prefix is required")
			}
			mapped.SetPrefix(prefix)
		}
		fields = append(fields, mapped)
	}
	return uiv1.ToolCallPreview_builder{
		CallId:      new(preview.CallID),
		Name:        new(preview.Name),
		Position:    new(int64(preview.Position)),
		Provisional: new(preview.Provisional),
		Fields:      fields,
	}.Build(), nil
}

func mapModelResponse(response controllerui.ModelResponse) (*uiv1.ModelResponse, error) {
	content, err := lo.MapErr(
		response.Content,
		func(item controllerui.ModelResponseContent, _ int) (*uiv1.ModelResponseContent, error) {
			var call *uiv1.FinalToolCall
			if value, present := item.ToolCall.Get(); present {
				arguments, mapErr := structpb.NewStruct(value.Arguments)
				if mapErr != nil {
					return nil, fmt.Errorf("map restored tool call arguments: %w", mapErr)
				}
				position := int64(value.Position)
				call = uiv1.FinalToolCall_builder{
					CallId: new(value.CallID), Name: new(value.Name),
					Position: new(position), Arguments: arguments,
				}.Build()
			}
			return uiv1.ModelResponseContent_builder{
				Kind: new(mapModelContentKind(item.Kind)), Text: new(item.Text), ToolCall: call,
			}.Build(), nil
		},
	)
	if err != nil {
		return nil, err
	}
	diagnostics := lo.Map(
		response.Diagnostics,
		func(diagnostic controllerui.ModelDiagnostic, _ int) *uiv1.ModelDiagnostic {
			return uiv1.ModelDiagnostic_builder{
				Code:    new(diagnostic.Code),
				Message: new(diagnostic.Message),
			}.Build()
		},
	)
	var outcome *string
	if value, present := response.Outcome.Get(); present {
		outcome = new(value)
	}
	var errorMessage *string
	if value, present := response.ErrorMessage.Get(); present {
		errorMessage = new(value)
	}
	var provider *string
	if value, present := response.Provider.Get(); present {
		provider = new(value)
	}
	var configuredModel *string
	if value, present := response.Model.Get(); present {
		configuredModel = new(value)
	}
	var responseModel *string
	if value, present := response.ResponseModel.Get(); present {
		responseModel = new(value)
	}
	var responseID *string
	if value, present := response.ResponseID.Get(); present {
		responseID = new(value)
	}
	var usage *uiv1.ModelUsage
	if value, present := response.Usage.Get(); present {
		usage = uiv1.ModelUsage_builder{
			InputTokens:       new(value.InputTokens),
			OutputTokens:      new(value.OutputTokens),
			CachedInputTokens: new(value.CachedInputTokens),
			CacheWriteTokens:  new(value.CacheWriteTokens),
			ReasoningTokens:   new(value.ReasoningTokens),
			TotalTokens:       new(value.TotalTokens),
		}.Build()
	}
	return uiv1.ModelResponse_builder{
		Text:          new(response.Text),
		Outcome:       outcome,
		ErrorMessage:  errorMessage,
		Provider:      provider,
		Model:         configuredModel,
		ResponseModel: responseModel,
		ResponseId:    responseID,
		Usage:         usage,
		Diagnostics:   diagnostics,
		Content:       content,
	}.Build(), nil
}

func mapModelContentKind(value controllerui.ModelContentKind) uiv1.ModelContentKind {
	switch value {
	case controllerui.ModelContentKindText:
		return uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT
	case controllerui.ModelContentKindRefusal:
		return uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL
	case controllerui.ModelContentKindReasoning:
		return uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING
	default:
		return uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED
	}
}

func mapModelContentType(value controllerui.ModelContentType) uiv1.ModelContentType {
	switch value {
	case controllerui.ModelContentStart:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_START
	case controllerui.ModelContentTextDelta:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA
	case controllerui.ModelContentEnd:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_END
	default:
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED
	}
}

// mapProgressChannel converts Host tool progress identity to the public contract.
func mapProgressChannel(value controllerui.ProgressChannel) uiv1.ProgressChannel {
	return uiv1.ProgressChannel(value)
}
