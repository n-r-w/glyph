package programmatic

import (
	"strings"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

func mapHistory(history []agent.HistoryEntry) []controller.HistoryEntry {
	entries := make([]controller.HistoryEntry, 0, len(history))
	for index := range history {
		entry := &history[index]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			entries = append(entries, controller.HistoryEntry{
				Kind: controller.HistoryEntryUser, UserText: publicInputText(entry.User),
				Model: emptyModelResponse(), ToolResult: emptyToolResult(),
			})
		case agent.HistoryEntryModel:
			entries = append(entries, controller.HistoryEntry{
				Kind: controller.HistoryEntryModel, UserText: "", Model: mapModelResponse(entry.Model),
				ToolResult: emptyToolResult(),
			})
		case agent.HistoryEntryToolResult:
			entries = append(entries, controller.HistoryEntry{
				Kind: controller.HistoryEntryToolResult, UserText: "", Model: emptyModelResponse(),
				ToolResult: mapToolResult(entry.ToolResult),
			})
		}
	}
	return entries
}

func publicInputText(message model.Message) string {
	var text strings.Builder
	for _, content := range message.Content {
		if content.Kind == model.InputContentText {
			text.WriteString(content.Text)
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
			text.WriteString(item.Text)
		}
	}
	var responseModel *string
	if response.ResponseModel != nil {
		value := string(*response.ResponseModel)
		responseModel = &value
	}
	diagnostics := make([]controller.ModelDiagnostic, len(response.Diagnostics))
	for index, diagnostic := range response.Diagnostics {
		diagnostics[index] = controller.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	}
	return controller.ModelResponse{
		Text:          text.String(),
		Outcome:       mapModelOutcome(response.Outcome),
		ErrorMessage:  response.ErrorMessage,
		Provider:      string(response.Provider),
		Model:         string(response.Model),
		ResponseModel: responseModel,
		ResponseID:    response.ResponseID,
		Usage: controller.ModelUsage{
			InputTokens:       response.Usage.InputTokens,
			OutputTokens:      response.Usage.OutputTokens,
			CachedInputTokens: response.Usage.CachedInputTokens,
			CacheWriteTokens:  response.Usage.CacheWriteTokens,
			ReasoningTokens:   response.Usage.ReasoningTokens,
			TotalTokens:       response.Usage.TotalTokens,
		},
		Diagnostics: diagnostics,
		Content:     content,
	}
}

func mapModelResponseContent(position int, content model.Content) (controller.ModelResponseContent, bool) {
	switch content.Kind {
	case model.ContentText:
		return controller.ModelResponseContent{
			Kind: controller.ModelResponseContentText, Text: content.Text, ToolCall: emptyFinalToolCall(),
		}, true
	case model.ContentRefusal:
		return controller.ModelResponseContent{
			Kind: controller.ModelResponseContentRefusal, Text: content.Text, ToolCall: emptyFinalToolCall(),
		}, true
	case model.ContentReasoning:
		return controller.ModelResponseContent{
			Kind: controller.ModelResponseContentReasoning, Text: content.Text, ToolCall: emptyFinalToolCall(),
		}, true
	case model.ContentToolCall:
		return controller.ModelResponseContent{
			Kind: controller.ModelResponseContentToolCall, Text: "",
			ToolCall: controller.FinalToolCall{
				CallID: content.ToolCall.ID, Name: content.ToolCall.Name, Position: position,
				Arguments: cloneArguments(content.ToolCall.Arguments),
			},
		}, true
	}
	return emptyModelResponseContent(), false
}

func mapToolCallPreview(preview model.ToolCallPreview) controller.ToolCallPreview {
	fields := make([]controller.ToolCallPreviewField, len(preview.Fields))
	for index, field := range preview.Fields {
		fields[index] = controller.ToolCallPreviewField{
			Name: field.Name, Kind: controller.ToolCallPreviewFieldUnspecified, Value: nil, Prefix: "",
		}
		switch field.Kind {
		case model.ToolCallPreviewFieldComplete:
			fields[index].Kind = controller.ToolCallPreviewFieldComplete
			fields[index].Value = cloneJSONValue(field.Value)
		case model.ToolCallPreviewFieldPrefix:
			fields[index].Kind = controller.ToolCallPreviewFieldPrefix
			fields[index].Prefix = field.Prefix
		}
	}
	return controller.ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

func mapToolResult(result agent.ToolResult) controller.ToolResult {
	contents := make([]controller.ToolResultContent, 0, len(result.Contents))
	for _, content := range result.Contents {
		switch content.Kind {
		case tool.ResultContentText:
			contents = append(contents, controller.ToolResultContent{
				Kind: controller.ToolResultContentText, Text: content.Text,
				Image: controller.ToolResultImage{MediaType: "", Data: nil},
			})
		case tool.ResultContentImage:
			contents = append(contents, controller.ToolResultContent{
				Kind: controller.ToolResultContentImage, Text: "",
				Image: controller.ToolResultImage{
					MediaType: content.Image.MediaType,
					Data:      append([]byte(nil), content.Image.Data...),
				},
			})
		}
	}
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

func emptyModelResponse() controller.ModelResponse {
	return controller.ModelResponse{
		Text: "", Outcome: controller.ModelOutcomeUnspecified, ErrorMessage: "", Provider: "", Model: "",
		ResponseModel: nil, ResponseID: "",
		Usage: controller.ModelUsage{
			InputTokens: 0, OutputTokens: 0, CachedInputTokens: 0,
			CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
		},
		Diagnostics: nil, Content: nil,
	}
}

func emptyModelResponseContent() controller.ModelResponseContent {
	return controller.ModelResponseContent{
		Kind: controller.ModelResponseContentUnspecified, Text: "", ToolCall: emptyFinalToolCall(),
	}
}

func emptyFinalToolCall() controller.FinalToolCall {
	return controller.FinalToolCall{CallID: "", Name: "", Position: 0, Arguments: nil}
}

func emptyToolResult() controller.ToolResult {
	return controller.ToolResult{CallID: "", ToolName: "", Contents: nil, IsError: false}
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := make(map[string]any, len(arguments))
	for key, value := range arguments {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	case []byte:
		return append([]byte(nil), typed...)
	default:
		return typed
	}
}
