package runtime

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/structpb"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapLifecycleType converts Host lifecycle identity to the public contract.
//
//nolint:gocyclo // The flat switch maps the complete lifecycle enum.
func mapLifecycleType(value domainui.LifecycleType) uipb.LifecycleType {
	switch value {
	case domainui.LifecycleAgentStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_START
	case domainui.LifecycleTurnStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TURN_START
	case domainui.LifecycleMessageStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START
	case domainui.LifecycleModelContentStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START
	case domainui.LifecycleModelTextDelta:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA
	case domainui.LifecycleModelContentEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END
	case domainui.LifecycleToolCallStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START
	case domainui.LifecycleToolCallDelta:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA
	case domainui.LifecycleToolCallEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END
	case domainui.LifecycleMessageEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END
	case domainui.LifecycleToolExecutionStart:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START
	case domainui.LifecycleToolExecutionUpdate:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE
	case domainui.LifecycleToolExecutionEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END
	case domainui.LifecycleToolResult:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT
	case domainui.LifecycleTurnEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_TURN_END
	case domainui.LifecycleAgentEnd:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_END
	case domainui.LifecycleAgentSettled:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED
	case domainui.LifecycleAvailabilityChanged:
		return uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED
	default:
		return uipb.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED
	}
}

// mapToolResultContents copies ordered domain blocks into the public UI contract.
func mapToolResultContents(contents []tool.ResultContent) []*uipb.ToolResultContent {
	return lo.FilterMap(contents, func(content tool.ResultContent, _ int) (*uipb.ToolResultContent, bool) {
		switch content.Kind {
		case tool.ResultContentText:
			text, present := content.Text.Get()
			if !present {
				return nil, false
			}
			//nolint:exhaustruct_v5 // uipb.ToolResultContent_builder sets only the active Text field.
			return uipb.ToolResultContent_builder{
				Text: &text,
			}.Build(), true
		case tool.ResultContentImage:
			image, present := content.Image.Get()
			if !present {
				return nil, false
			}
			mappedImage := uipb.ToolResultImage_builder{
				MediaType: &image.MediaType,
				Data:      nil,
			}.Build()
			mappedImage.SetData(bytes.Clone(image.Data))
			//nolint:exhaustruct_v5 // uipb.ToolResultContent_builder sets only the active Image field.
			return uipb.ToolResultContent_builder{
				Image: mappedImage,
			}.Build(), true
		}
		return nil, false
	})
}

func mapToolCallPreview(preview domainui.ToolCallPreview) (*uipb.ToolCallPreview, error) {
	fields := make([]*uipb.ToolCallPreviewField, 0, len(preview.Fields))
	for _, field := range preview.Fields {
		mapped := uipb.ToolCallPreviewField_builder{
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
	return uipb.ToolCallPreview_builder{
		CallId:      new(preview.CallID),
		Name:        new(preview.Name),
		Position:    new(int32(preview.Position)), //nolint:gosec // Positions are bounded by response size.
		Provisional: new(preview.Provisional),
		Fields:      fields,
	}.Build(), nil
}

func mapModelResponse(response domainui.ModelResponse) (*uipb.ModelResponse, error) {
	content, err := lo.MapErr(
		response.Content,
		func(item domainui.ModelResponseContent, _ int) (*uipb.ModelResponseContent, error) {
			var call *uipb.FinalToolCall
			if value, present := item.ToolCall.Get(); present {
				arguments, mapErr := structpb.NewStruct(value.Arguments)
				if mapErr != nil {
					return nil, fmt.Errorf("map restored tool call arguments: %w", mapErr)
				}
				position := int32(value.Position) //nolint:gosec // Response content bounds the position.
				call = uipb.FinalToolCall_builder{
					CallId: new(value.CallID), Name: new(value.Name),
					Position: new(position), Arguments: arguments,
				}.Build()
			}
			return uipb.ModelResponseContent_builder{
				Kind: new(mapModelContentKind(item.Kind)), Text: new(item.Text), ToolCall: call,
			}.Build(), nil
		},
	)
	if err != nil {
		return nil, err
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic domainui.ModelDiagnostic, _ int) *uipb.ModelDiagnostic {
		return uipb.ModelDiagnostic_builder{
			Code:    new(diagnostic.Code),
			Message: new(diagnostic.Message),
		}.Build()
	})
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
	var usage *uipb.ModelUsage
	if value, present := response.Usage.Get(); present {
		usage = uipb.ModelUsage_builder{
			InputTokens:       new(value.InputTokens),
			OutputTokens:      new(value.OutputTokens),
			CachedInputTokens: new(value.CachedInputTokens),
			CacheWriteTokens:  new(value.CacheWriteTokens),
			ReasoningTokens:   new(value.ReasoningTokens),
			TotalTokens:       new(value.TotalTokens),
		}.Build()
	}
	return uipb.ModelResponse_builder{
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

func mapModelContentKind(value domainui.ModelContentKind) uipb.ModelContentKind {
	switch value {
	case domainui.ModelContentKindText:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_TEXT
	case domainui.ModelContentKindRefusal:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL
	case domainui.ModelContentKindReasoning:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_REASONING
	default:
		return uipb.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED
	}
}

func mapModelContentType(value domainui.ModelContentType) uipb.ModelContentType {
	switch value {
	case domainui.ModelContentStart:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_START
	case domainui.ModelContentTextDelta:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA
	case domainui.ModelContentEnd:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_END
	default:
		return uipb.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED
	}
}

// mapProgressChannel converts Host tool progress identity to the public contract.
func mapProgressChannel(value domainui.ProgressChannel) uipb.ProgressChannel {
	return uipb.ProgressChannel(value)
}
