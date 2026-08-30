package programmatic

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

func mapHistory(history []agent.HistoryEntry) ([]controller.HistoryEntry, error) {
	result := make([]controller.HistoryEntry, 0, len(history))
	for position := range history {
		entry := &history[position]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			user, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("map history entry %d: user payload is missing", position)
			}
			result = append(result, controller.HistoryEntry{
				Kind: controller.HistoryEntryUser, User: mo.Some(user.Clone()),
				Model:      mo.None[controller.ModelResponse](),
				ToolResult: mo.None[controller.ToolResult](),
			})
		case agent.HistoryEntryModel:
			response, present := entry.Model.Get()
			if !present {
				return nil, fmt.Errorf("map history entry %d: model payload is missing", position)
			}
			mapped, err := mapModelResponseProjection(response)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", position, err)
			}
			result = append(result, controller.HistoryEntry{
				Kind: controller.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(mapped), ToolResult: mo.None[controller.ToolResult](),
			})
		case agent.HistoryEntryToolResult:
			toolResult, present := entry.ToolResult.Get()
			if !present {
				return nil, fmt.Errorf("map history entry %d: tool result payload is missing", position)
			}
			publicToolResult := mapToolResult(toolResult)
			result = append(result, controller.HistoryEntry{
				Kind: controller.HistoryEntryToolResult, User: mo.None[model.Message](),
				Model: mo.None[controller.ModelResponse](), ToolResult: mo.Some(publicToolResult),
			})
		default:
			return nil, fmt.Errorf("map history entry %d: unknown kind %d", position, entry.Kind)
		}
	}
	return result, nil
}

func mapSessionEntries(entries []session.Entry) ([]controller.SessionEntry, error) {
	result := make([]controller.SessionEntry, 0, len(entries))
	for position := range entries {
		entry := &entries[position]
		if user, present := entry.User.Get(); present {
			result = append(result, controller.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: controller.HistoryEntryUser,
				User: mo.Some(user.Clone()), Model: mo.None[controller.ModelResponse](),
				EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[controller.ToolResult](),
				BranchSummary: mo.None[controller.BranchSummary](),
			})
			continue
		}
		if response, present := entry.Model.Get(); present {
			mapped, err := mapModelResponseProjection(response)
			if err != nil {
				return nil, fmt.Errorf("map session entry %d: %w", position, err)
			}
			result = append(result, controller.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: controller.HistoryEntryModel,
				User: mo.None[model.Message](), Model: mo.Some(mapped),
				EstimatedCost: entry.EstimatedCost, ToolResult: mo.None[controller.ToolResult](),
				BranchSummary: mo.None[controller.BranchSummary](),
			})
			continue
		}
		if toolResult, present := entry.ToolResult.Get(); present {
			result = append(result, controller.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: controller.HistoryEntryToolResult,
				User: mo.None[model.Message](), Model: mo.None[controller.ModelResponse](),
				EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.Some(mapToolResult(toolResult)),
				BranchSummary: mo.None[controller.BranchSummary](),
			})
			continue
		}
		if summary, present := entry.BranchSummary.Get(); present {
			result = append(result, controller.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: controller.HistoryEntryBranchSummary,
				User: mo.None[model.Message](), Model: mo.None[controller.ModelResponse](),
				EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[controller.ToolResult](),
				BranchSummary: mo.Some(controller.BranchSummary{
					Summary: summary.Summary, FirstEntryID: summary.FirstEntryID, LastEntryID: summary.LastEntryID,
					Provider: summary.Provider, Model: summary.Model, ReasoningChoice: summary.ReasoningChoice,
					Usage: summary.Usage, EstimatedCost: summary.EstimatedCost,
				}),
			})
		}
	}
	return result, nil
}

func mapModelResponseProjection(response model.Response) (controller.ModelResponse, error) {
	if err := response.ValidateTerminalContent(); err != nil {
		return controller.ModelResponse{}, fmt.Errorf("map model response: %w", err)
	}
	mappedContent, err := lo.MapErr(response.Content, func(
		item model.Content,
		position int,
	) (mo.Option[controller.ModelResponseContent], error) {
		return mapModelResponseContent(position, item)
	})
	if err != nil {
		return controller.ModelResponse{}, err
	}
	content := make([]controller.ModelResponseContent, 0, len(mappedContent))
	var text strings.Builder
	for position := range mappedContent {
		mapped, present := mappedContent[position].Get()
		if !present {
			continue
		}
		content = append(content, mapped)
		if mapped.Kind == controller.ModelResponseContentText || mapped.Kind == controller.ModelResponseContentRefusal {
			mappedText, hasText := mapped.Text.Get()
			if !hasText {
				return controller.ModelResponse{}, fmt.Errorf(
					"map model response content %d: text is missing",
					position,
				)
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
	diagnostics := mapModelDiagnostics(response.Diagnostics)
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
	}, nil
}

// mapModelDiagnostics copies restored diagnostics into the public response.
func mapModelDiagnostics(diagnostics []model.Diagnostic) []controller.ModelDiagnostic {
	return lo.Map(diagnostics, func(diagnostic model.Diagnostic, _ int) controller.ModelDiagnostic {
		return controller.ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
}

func mapModelResponseContent(
	position int,
	content model.Content,
) (mo.Option[controller.ModelResponseContent], error) {
	switch content.Kind {
	case model.ContentText, model.ContentRefusal, model.ContentReasoning:
		text, hasText := content.Text.Get()
		if !hasText {
			if content.Kind == model.ContentReasoning && content.ProviderContext.IsSome() {
				return mo.None[controller.ModelResponseContent](), nil
			}
			return mo.None[controller.ModelResponseContent](), errors.New("model response content text is missing")
		}
		kind := controller.ModelResponseContentText
		switch content.Kind {
		case model.ContentRefusal:
			kind = controller.ModelResponseContentRefusal
		case model.ContentReasoning:
			kind = controller.ModelResponseContentReasoning
		case model.ContentText, model.ContentToolCall:
		}
		return mo.Some(controller.ModelResponseContent{
			Kind: kind, Text: mo.Some(text), ToolCall: mo.None[controller.FinalToolCall](),
		}), nil
	case model.ContentToolCall:
		call, hasToolCall := content.ToolCall.Get()
		if !hasToolCall {
			return mo.None[controller.ModelResponseContent](), errors.New("model response tool call is missing")
		}
		return mo.Some(controller.ModelResponseContent{
			Kind: controller.ModelResponseContentToolCall, Text: mo.None[string](),
			ToolCall: mo.Some(controller.FinalToolCall{
				CallID: call.ID, Name: call.Name, Position: position,
				Arguments: call.Clone().Arguments,
			}),
		}), nil
	}
	return mo.None[controller.ModelResponseContent](), fmt.Errorf(
		"unknown model response content kind %d",
		content.Kind,
	)
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
			mapped.Value = field.Clone().Value
		case model.ToolCallPreviewFieldPrefix:
			mapped.Kind = controller.ToolCallPreviewFieldPrefix
			mapped.Prefix = field.Prefix
		}
		return mapped
	})
	return controller.ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

// mapToolResult preserves valid ordered text and image blocks with owned image bytes.
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
