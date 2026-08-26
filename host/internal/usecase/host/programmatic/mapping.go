package programmatic

import (
	"bytes"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

func mapHistory(history []agent.HistoryEntry) []controller.HistoryEntry {
	return lo.FilterMap(history, func(entry agent.HistoryEntry, _ int) (controller.HistoryEntry, bool) {
		switch entry.Kind {
		case agent.HistoryEntryUser:
			user, ok := entry.User.Get()
			if !ok {
				return controller.HistoryEntry{}, false
			}
			return controller.HistoryEntry{
				Kind:       controller.HistoryEntryUser,
				UserText:   mo.Some(publicInputText(user)),
				Model:      mo.None[controller.ModelResponse](),
				ToolResult: mo.None[controller.ToolResult](),
			}, true
		case agent.HistoryEntryModel:
			modelResponse, ok := entry.Model.Get()
			if !ok {
				return controller.HistoryEntry{}, false
			}
			return controller.HistoryEntry{
				Kind:       controller.HistoryEntryModel,
				UserText:   mo.None[string](),
				Model:      mo.Some(mapModelResponse(modelResponse)),
				ToolResult: mo.None[controller.ToolResult](),
			}, true
		case agent.HistoryEntryToolResult:
			toolResult, ok := entry.ToolResult.Get()
			if !ok {
				return controller.HistoryEntry{}, false
			}
			return controller.HistoryEntry{
				Kind:       controller.HistoryEntryToolResult,
				UserText:   mo.None[string](),
				Model:      mo.None[controller.ModelResponse](),
				ToolResult: mo.Some(mapToolResult(toolResult)),
			}, true
		}
		return controller.HistoryEntry{}, false
	})
}

func publicInputText(message model.Message) string {
	var text strings.Builder
	for _, content := range message.Content {
		if content.Kind != model.InputContentText {
			continue
		}
		contentText, ok := content.Text.Get()
		if ok {
			text.WriteString(contentText)
		}
	}
	return text.String()
}

func mapModelResponse(response model.Response) controller.ModelResponse {
	content := make([]controller.ModelResponseContent, 0, len(response.Content))
	var text strings.Builder
	for position := range response.Content {
		item := &response.Content[position]
		if !item.Final {
			continue
		}
		mapped, ok := mapModelResponseContent(position, *item)
		if !ok {
			continue
		}
		content = append(content, mapped)
		if item.Kind == model.ContentText || item.Kind == model.ContentRefusal {
			mappedText, present := mapped.Text.Get()
			if !present {
				continue
			}
			text.WriteString(mappedText)
		}
	}
	responseModel := mo.None[string]()
	if actualModel, ok := response.ResponseModel.Get(); ok {
		responseModel = mo.Some(string(actualModel))
	}
	provider := mo.None[string]()
	if providerID, ok := response.Provider.Get(); ok {
		provider = mo.Some(string(providerID))
	}
	configuredModel := mo.None[string]()
	if modelID, ok := response.Model.Get(); ok {
		configuredModel = mo.Some(string(modelID))
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic model.Diagnostic, _ int) controller.ModelDiagnostic {
		return controller.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
	outcome := mo.None[controller.ModelOutcome]()
	if modelOutcome, ok := response.Outcome.Get(); ok {
		outcome = mo.Some(mapModelOutcome(modelOutcome))
	}
	usage := mo.None[controller.ModelUsage]()
	if modelUsage, ok := response.Usage.Get(); ok {
		usage = mo.Some(controller.ModelUsage{
			InputTokens:       modelUsage.InputTokens,
			OutputTokens:      modelUsage.OutputTokens,
			CachedInputTokens: modelUsage.CachedInputTokens,
			CacheWriteTokens:  modelUsage.CacheWriteTokens,
			ReasoningTokens:   modelUsage.ReasoningTokens,
			TotalTokens:       modelUsage.TotalTokens,
		})
	}
	return controller.ModelResponse{
		Text:          text.String(),
		Outcome:       outcome,
		ErrorMessage:  response.ErrorMessage,
		Provider:      provider,
		Model:         configuredModel,
		ResponseModel: responseModel,
		ResponseID:    response.ResponseID,
		Usage:         usage,
		Diagnostics:   diagnostics,
		Content:       content,
	}
}

func mapModelResponseContent(position int, content model.Content) (controller.ModelResponseContent, bool) {
	switch content.Kind {
	case model.ContentText, model.ContentRefusal, model.ContentReasoning:
		text, ok := content.Text.Get()
		if !ok {
			return controller.ModelResponseContent{}, false
		}
		kind := controller.ModelResponseContentText
		switch content.Kind {
		case model.ContentRefusal:
			kind = controller.ModelResponseContentRefusal
		case model.ContentReasoning:
			kind = controller.ModelResponseContentReasoning
		case model.ContentText, model.ContentToolCall:
		}
		return controller.ModelResponseContent{
			Kind: kind, Text: mo.Some(text), ToolCall: mo.None[controller.FinalToolCall](),
		}, true
	case model.ContentToolCall:
		call, ok := content.ToolCall.Get()
		if !ok {
			return controller.ModelResponseContent{}, false
		}
		return controller.ModelResponseContent{
			Kind: controller.ModelResponseContentToolCall, Text: mo.None[string](),
			ToolCall: mo.Some(controller.FinalToolCall{
				CallID: call.ID, Name: call.Name, Position: position,
				Arguments: cloneArguments(call.Arguments),
			}),
		}, true
	}
	return controller.ModelResponseContent{}, false
}

func mapToolCallPreview(preview model.ToolCallPreview) controller.ToolCallPreview {
	fields := lo.Map(preview.Fields, func(field model.ToolCallPreviewField, _ int) controller.ToolCallPreviewField {
		mapped := controller.ToolCallPreviewField{
			Name: field.Name, Kind: controller.ToolCallPreviewFieldUnspecified,
			Value: mo.None[any](), Prefix: mo.None[string](),
		}
		switch field.Kind {
		case model.ToolCallPreviewFieldComplete:
			mapped.Kind = controller.ToolCallPreviewFieldComplete
			mapped.Value = mo.Some(cloneJSONValue(field.Value))
		case model.ToolCallPreviewFieldPrefix:
			mapped.Kind = controller.ToolCallPreviewFieldPrefix
			mapped.Prefix = mo.Some(field.Prefix)
		}
		return mapped
	})
	return controller.ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

func mapToolResult(result agent.ToolResult) controller.ToolResult {
	contents := lo.FilterMap(
		result.Contents,
		func(content tool.ResultContent, _ int) (controller.ToolResultContent, bool) {
			switch content.Kind {
			case tool.ResultContentText:
				text, ok := content.Text.Get()
				if !ok {
					return controller.ToolResultContent{}, false
				}
				return controller.ToolResultContent{
					Kind: controller.ToolResultContentText, Text: mo.Some(text),
					Image: mo.None[controller.ToolResultImage](),
				}, true
			case tool.ResultContentImage:
				image, ok := content.Image.Get()
				if !ok {
					return controller.ToolResultContent{}, false
				}
				return controller.ToolResultContent{
					Kind: controller.ToolResultContentImage, Text: mo.None[string](),
					Image: mo.Some(controller.ToolResultImage{
						MediaType: image.MediaType,
						Data:      bytes.Clone(image.Data),
					}),
				}, true
			}
			return controller.ToolResultContent{}, false
		},
	)
	return controller.ToolResult{
		CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
	}
}

func mapModelContentKind(kind model.ContentKind) controller.ModelContentKind {
	switch kind {
	case model.ContentText:
		return controller.ModelContentText
	case model.ContentReasoning:
		return controller.ModelContentReasoning
	case model.ContentRefusal:
		return controller.ModelContentRefusal
	case model.ContentToolCall:
		return controller.ModelContentUnspecified
	}
	return controller.ModelContentUnspecified
}

func mapProgressChannel(channel tool.ProgressChannel) controller.ProgressChannel {
	switch channel {
	case tool.ProgressChannelStatus:
		return controller.ProgressChannelStatus
	case tool.ProgressChannelStdout:
		return controller.ProgressChannelStdout
	case tool.ProgressChannelStderr:
		return controller.ProgressChannelStderr
	}
	return controller.ProgressChannelUnspecified
}

func mapRunOutcome(outcome agent.RunOutcome) controller.RunOutcome {
	switch outcome {
	case agent.RunOutcomeCompleted:
		return controller.RunOutcomeCompleted
	case agent.RunOutcomeAborted:
		return controller.RunOutcomeAborted
	case agent.RunOutcomeFailed:
		return controller.RunOutcomeFailed
	}
	return controller.RunOutcomeUnspecified
}

func mapModelOutcome(outcome model.Outcome) controller.ModelOutcome {
	switch outcome {
	case model.OutcomeStop:
		return controller.ModelOutcomeStop
	case model.OutcomeToolUse:
		return controller.ModelOutcomeToolUse
	case model.OutcomeLength:
		return controller.ModelOutcomeLength
	case model.OutcomeAborted:
		return controller.ModelOutcomeAborted
	case model.OutcomeFailed:
		return controller.ModelOutcomeFailed
	}
	return controller.ModelOutcomeUnspecified
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := maps.Clone(arguments)
	for key, value := range cloned {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index, item := range cloned {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	case []byte:
		return bytes.Clone(typed)
	default:
		return typed
	}
}
