package programmatic

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// MapModelResponseProjection validates terminal content and excludes opaque provider state.
func MapModelResponseProjection(response model.Response) (ModelResponse, error) {
	if err := response.ValidateTerminalContent(); err != nil {
		return ModelResponse{}, fmt.Errorf("map model response: %w", err)
	}
	mappedContent, err := lo.MapErr(response.Content, func(
		item model.Content,
		position int,
	) (mo.Option[ModelResponseContent], error) {
		return MapModelResponseContent(position, item)
	})
	if err != nil {
		return ModelResponse{}, err
	}
	content := make([]ModelResponseContent, 0, len(mappedContent))
	var text strings.Builder
	for position := range mappedContent {
		mapped, present := mappedContent[position].Get()
		if !present {
			continue
		}
		content = append(content, mapped)
		if mapped.Kind == ModelResponseContentText || mapped.Kind == ModelResponseContentRefusal {
			mappedText, hasText := mapped.Text.Get()
			if !hasText {
				return ModelResponse{}, fmt.Errorf(
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
	diagnostics := MapModelDiagnostics(response.Diagnostics)
	outcome := mo.None[ModelOutcome]()
	if modelOutcome, ok := response.Outcome.Get(); ok {
		outcome = mo.Some(MapModelOutcome(modelOutcome))
	}
	usage := mo.None[ModelUsage]()
	if modelUsage, ok := response.Usage.Get(); ok {
		usage = mo.Some(ModelUsage{
			InputTokens:       modelUsage.InputTokens,
			OutputTokens:      modelUsage.OutputTokens,
			CachedInputTokens: modelUsage.CachedInputTokens,
			CacheWriteTokens:  modelUsage.CacheWriteTokens,
			ReasoningTokens:   modelUsage.ReasoningTokens,
			TotalTokens:       modelUsage.TotalTokens,
		})
	}
	return ModelResponse{
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

// MapModelDiagnostics copies restored diagnostics into the public response.
func MapModelDiagnostics(diagnostics []model.Diagnostic) []ModelDiagnostic {
	return lo.Map(diagnostics, func(diagnostic model.Diagnostic, _ int) ModelDiagnostic {
		return ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
}

// MapModelResponseContent projects one public content block with its original position.
func MapModelResponseContent(
	position int,
	content model.Content,
) (mo.Option[ModelResponseContent], error) {
	switch content.Kind {
	case model.ContentText, model.ContentRefusal, model.ContentReasoning:
		text, hasText := content.Text.Get()
		if !hasText {
			if content.Kind == model.ContentReasoning && content.ProviderContext.IsSome() {
				return mo.None[ModelResponseContent](), nil
			}
			return mo.None[ModelResponseContent](), errors.New("model response content text is missing")
		}
		kind := ModelResponseContentText
		switch content.Kind {
		case model.ContentRefusal:
			kind = ModelResponseContentRefusal
		case model.ContentReasoning:
			kind = ModelResponseContentReasoning
		case model.ContentText, model.ContentToolCall:
		}
		return mo.Some(ModelResponseContent{
			Kind: kind, Text: mo.Some(text), ToolCall: mo.None[FinalToolCall](),
		}), nil
	case model.ContentToolCall:
		call, hasToolCall := content.ToolCall.Get()
		if !hasToolCall {
			return mo.None[ModelResponseContent](), errors.New("model response tool call is missing")
		}
		return mo.Some(ModelResponseContent{
			Kind: ModelResponseContentToolCall, Text: mo.None[string](),
			ToolCall: mo.Some(FinalToolCall{
				CallID: call.ID, Name: call.Name, Position: position,
				Arguments: call.Clone().Arguments,
			}),
		}), nil
	}
	return mo.None[ModelResponseContent](), fmt.Errorf(
		"unknown model response content kind %d",
		content.Kind,
	)
}

// MapToolCallPreview copies provisional tool fields without sharing mutable values.
func MapToolCallPreview(preview model.ToolCallPreview) ToolCallPreview {
	fields := lo.Map(preview.Fields, func(field model.ToolCallPreviewField, _ int) ToolCallPreviewField {
		mapped := ToolCallPreviewField{
			Name: field.Name, Kind: ToolCallPreviewFieldUnspecified,
			Value: mo.None[any](), Prefix: mo.None[string](),
		}
		switch field.Kind {
		case model.ToolCallPreviewFieldComplete:
			mapped.Kind = ToolCallPreviewFieldComplete
			mapped.Value = field.Clone().Value
		case model.ToolCallPreviewFieldPrefix:
			mapped.Kind = ToolCallPreviewFieldPrefix
			mapped.Prefix = field.Prefix
		}
		return mapped
	})
	return ToolCallPreview{
		CallID: preview.CallID, Name: preview.Name, Position: preview.Position,
		Provisional: preview.Provisional, Fields: fields,
	}
}

// MapToolResult preserves valid ordered text and image blocks with owned image bytes.
func MapToolResult(result agent.ToolResult) ToolResult {
	contents := lo.FilterMap(
		result.Contents,
		func(content tool.ResultContent, _ int) (ToolResultContent, bool) {
			switch content.Kind {
			case tool.ResultContentText:
				text, ok := content.Text.Get()
				if !ok {
					return ToolResultContent{}, false
				}
				return ToolResultContent{
					Kind: ToolResultContentText, Text: mo.Some(text),
					Image: mo.None[ToolResultImage](),
				}, true
			case tool.ResultContentImage:
				image, ok := content.Image.Get()
				if !ok {
					return ToolResultContent{}, false
				}
				return ToolResultContent{
					Kind: ToolResultContentImage, Text: mo.None[string](),
					Image: mo.Some(ToolResultImage{
						MediaType: image.MediaType,
						Data:      bytes.Clone(image.Data),
					}),
				}, true
			}
			return ToolResultContent{}, false
		},
	)
	return ToolResult{
		CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
	}
}

// MapModelContentKind maps a provider-neutral content kind to its client discriminator.
func MapModelContentKind(kind model.ContentKind) ModelContentKind {
	switch kind {
	case model.ContentText:
		return ModelContentText
	case model.ContentReasoning:
		return ModelContentReasoning
	case model.ContentRefusal:
		return ModelContentRefusal
	case model.ContentToolCall:
		return ModelContentUnspecified
	}
	return ModelContentUnspecified
}

// MapProgressChannel preserves the meaning of tool progress output.
func MapProgressChannel(channel tool.ProgressChannel) ProgressChannel {
	switch channel {
	case tool.ProgressChannelStatus:
		return ProgressChannelStatus
	case tool.ProgressChannelStdout:
		return ProgressChannelStdout
	case tool.ProgressChannelStderr:
		return ProgressChannelStderr
	}
	return ProgressChannelUnspecified
}

// MapRunOutcome maps a domain run outcome to its public category.
func MapRunOutcome(outcome agent.RunOutcome) RunOutcome {
	switch outcome {
	case agent.RunOutcomeCompleted:
		return RunOutcomeCompleted
	case agent.RunOutcomeAborted:
		return RunOutcomeAborted
	case agent.RunOutcomeFailed:
		return RunOutcomeFailed
	}
	return RunOutcomeUnspecified
}

// MapModelOutcome maps a terminal model outcome to its public category.
func MapModelOutcome(outcome model.Outcome) ModelOutcome {
	switch outcome {
	case model.OutcomeStop:
		return ModelOutcomeStop
	case model.OutcomeToolUse:
		return ModelOutcomeToolUse
	case model.OutcomeLength:
		return ModelOutcomeLength
	case model.OutcomeAborted:
		return ModelOutcomeAborted
	case model.OutcomeFailed:
		return ModelOutcomeFailed
	}
	return ModelOutcomeUnspecified
}

// ProjectSessionEntry constructs one public entry and identifies records without a conversation payload.
func ProjectSessionEntry(entry session.Entry, position int) (SessionEntry, bool, error) {
	if user, present := entry.User.Get(); present {
		return SessionEntry{
			ID:               entry.ID,
			CreatedAt:        entry.CreatedAt,
			Kind:             HistoryEntryUser,
			User:             mo.Some(user.Clone()),
			Model:            mo.None[ModelResponse](),
			EstimatedCost:    mo.None[session.EstimatedCost](),
			ToolResult:       mo.None[ToolResult](),
			BranchSummary:    mo.None[BranchSummary](),
			ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}
	if response, present := entry.Model.Get(); present {
		mapped, err := MapModelResponseProjection(response)
		if err != nil {
			return SessionEntry{}, false, fmt.Errorf("map session entry %d: %w", position, err)
		}
		return SessionEntry{
			ID:               entry.ID,
			CreatedAt:        entry.CreatedAt,
			Kind:             HistoryEntryModel,
			User:             mo.None[model.Message](),
			Model:            mo.Some(mapped),
			EstimatedCost:    entry.EstimatedCost,
			ToolResult:       mo.None[ToolResult](),
			BranchSummary:    mo.None[BranchSummary](),
			ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}
	if toolResult, present := entry.ToolResult.Get(); present {
		return SessionEntry{
			ID:               entry.ID,
			CreatedAt:        entry.CreatedAt,
			Kind:             HistoryEntryToolResult,
			User:             mo.None[model.Message](),
			Model:            mo.None[ModelResponse](),
			EstimatedCost:    mo.None[session.EstimatedCost](),
			ToolResult:       mo.Some(MapToolResult(toolResult)),
			BranchSummary:    mo.None[BranchSummary](),
			ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}
	if message, present := entry.ExtensionMessage.Get(); present {
		return SessionEntry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: HistoryEntryExtensionMessage,
			User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
			BranchSummary: mo.None[BranchSummary](),
			ExtensionMessage: mo.Some(ExtensionMessage{
				ExtensionID: message.ExtensionID, EntryType: message.EntryType,
				Text: message.Text, Visibility: message.Visibility,
			}),
		}, true, nil
	}
	if summary, present := entry.BranchSummary.Get(); present {
		return SessionEntry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: HistoryEntryBranchSummary,
			User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
			BranchSummary: mo.Some(BranchSummary{
				Summary: summary.Summary, FirstEntryID: summary.FirstEntryID, LastEntryID: summary.LastEntryID,
				Source: summary.Source, EstimatedCost: summary.EstimatedCost,
			}), ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}

	return SessionEntry{}, false, nil
}

// ProjectSessionTreeEntry maps one closed tree payload without extension data.
func ProjectSessionTreeEntry(entry session.Entry, label string) (SessionTreeEntry, error) {
	mapped := SessionTreeEntry{
		ID: entry.ID, ParentID: entry.ParentID, CreatedAt: entry.CreatedAt, Label: label,
		Kind: SessionTreeEntryUnspecified,
		User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
		Extension: mo.None[ExtensionEntry](), BranchSummary: mo.None[BranchSummary](),
		ExtensionMessage: mo.None[ExtensionMessage](),
	}
	if extension, present := entry.Extension.Get(); present {
		mapped.Kind = SessionTreeEntryExtension
		mapped.Extension = mo.Some(ExtensionEntry{
			ExtensionID: extension.ExtensionID, EntryType: extension.EntryType,
		})
		return mapped, nil
	}
	if message, present := entry.ExtensionMessage.Get(); present {
		mapped.Kind = SessionTreeEntryExtensionMessage
		mapped.ExtensionMessage = mo.Some(ExtensionMessage{
			ExtensionID: message.ExtensionID, EntryType: message.EntryType,
			Text: message.Text, Visibility: message.Visibility,
		})
		return mapped, nil
	}
	public, present, err := ProjectSessionEntry(entry, 0)
	if err != nil {
		return SessionTreeEntry{}, err
	}
	if !present {
		return SessionTreeEntry{}, errors.New("tree entry has no public payload")
	}
	mapped.User = public.User
	mapped.Model = public.Model
	mapped.EstimatedCost = public.EstimatedCost
	mapped.ToolResult = public.ToolResult
	mapped.BranchSummary = public.BranchSummary
	mapped.ExtensionMessage = public.ExtensionMessage
	switch public.Kind {
	case HistoryEntryUser:
		mapped.Kind = SessionTreeEntryUser
	case HistoryEntryModel:
		mapped.Kind = SessionTreeEntryModel
	case HistoryEntryToolResult:
		mapped.Kind = SessionTreeEntryToolResult
	case HistoryEntryBranchSummary:
		mapped.Kind = SessionTreeEntryBranchSummary
	case HistoryEntryExtensionMessage:
		mapped.Kind = SessionTreeEntryExtensionMessage
	case HistoryEntryUnspecified:
		return SessionTreeEntry{}, errors.New("tree entry payload is unspecified")
	default:
		return SessionTreeEntry{}, fmt.Errorf("unknown tree entry payload %d", public.Kind)
	}
	return mapped, nil
}
