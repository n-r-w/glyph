package ui

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

const (
	// OutcomeTextCompleted identifies successful agent completion.
	OutcomeTextCompleted = "completed"
	// OutcomeTextStop identifies a normally stopped model response.
	OutcomeTextStop = "stop"
	// OutcomeTextToolUse identifies a model response that requests tools.
	OutcomeTextToolUse = "tool_use"
	// OutcomeTextLength identifies a model response stopped by its length limit.
	OutcomeTextLength = "length"

	// OutcomeTextAborted identifies an aborted model request or agent run.
	OutcomeTextAborted = "aborted"
	// OutcomeTextFailed identifies a failed model request or agent run.
	OutcomeTextFailed = "failed"
)

// ProjectModelResponse validates terminal content and projects the requested public history representation.
func ProjectModelResponse(
	response model.Response,
	continuationOnly bool,
) (ModelResponse, error) {
	if err := response.ValidateTerminalContent(); err != nil {
		return ModelResponse{}, fmt.Errorf("map UI model response: %w", err)
	}
	mappedContent, err := lo.MapErr(response.Content, func(
		item model.Content,
		position int,
	) (mo.Option[ModelResponseContent], error) {
		if continuationOnly && item.Kind != model.ContentText && item.Kind != model.ContentToolCall {
			return mo.None[ModelResponseContent](), nil
		}
		return projectModelResponseContent(position, item)
	})
	if err != nil {
		return ModelResponse{}, err
	}
	content := make([]ModelResponseContent, 0, len(mappedContent))
	for position := range mappedContent {
		if item, present := mappedContent[position].Get(); present {
			content = append(content, item)
		}
	}
	responseModel := mo.None[string]()
	if actualModel, ok := response.ResponseModel.Get(); ok {
		responseModel = mo.Some(string(actualModel))
	}
	diagnostics := lo.Map(response.Diagnostics, func(diagnostic model.Diagnostic, _ int) ModelDiagnostic {
		return ModelDiagnostic{Code: diagnostic.Code, Message: diagnostic.Message}
	})
	outcome := mo.None[string]()
	if value, present := response.Outcome.Get(); present {
		outcome = mo.Some(ModelOutcomeText(value))
	}
	errorMessage := mo.None[string]()
	if value, present := response.ErrorMessage.Get(); present {
		errorMessage = mo.Some(value)
	}
	provider := mo.None[string]()
	if value, present := response.Provider.Get(); present {
		provider = mo.Some(string(value))
	}
	configuredModel := mo.None[string]()
	if value, present := response.Model.Get(); present {
		configuredModel = mo.Some(string(value))
	}
	responseID := mo.None[string]()
	if value, present := response.ResponseID.Get(); present {
		responseID = mo.Some(value)
	}
	mappedUsage := mo.None[ModelUsage]()
	if usage, present := response.Usage.Get(); present {
		mappedUsage = mo.Some(ModelUsage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CachedInputTokens: usage.CachedInputTokens, CacheWriteTokens: usage.CacheWriteTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
		})
	}
	return ModelResponse{
		Text: response.Text(), Outcome: outcome, ErrorMessage: errorMessage,
		Provider: provider, Model: configuredModel, ResponseModel: responseModel,
		ResponseID: responseID, Content: content, Usage: mappedUsage, Diagnostics: diagnostics,
	}, nil
}

// projectModelResponseContent projects one valid terminal content item without opaque provider data.
func projectModelResponseContent(
	position int,
	item model.Content,
) (mo.Option[ModelResponseContent], error) {
	if item.Kind == model.ContentToolCall {
		call, present := item.ToolCall.Get()
		if !present {
			return mo.None[ModelResponseContent](), errors.New("UI model response tool call is missing")
		}
		return mo.Some(ModelResponseContent{
			Kind: ModelContentKind(0), Text: "",
			ToolCall: mo.Some(FinalToolCall{
				CallID: call.ID, Name: call.Name, Position: position, Arguments: call.Clone().Arguments,
			}),
		}), nil
	}
	kind := ModelContentKindFromDomain(item.Kind)
	if kind == 0 {
		return mo.None[ModelResponseContent](), nil
	}
	text, present := item.Text.Get()
	if !present {
		if item.Kind == model.ContentReasoning && item.ProviderContext.IsSome() {
			return mo.None[ModelResponseContent](), nil
		}
		return mo.None[ModelResponseContent](), errors.New("UI model response content text is missing")
	}
	return mo.Some(
		ModelResponseContent{Kind: kind, Text: text, ToolCall: mo.None[FinalToolCall]()},
	), nil
}

// ModelContentKindFromDomain maps only UI-safe streamed content kinds.
func ModelContentKindFromDomain(kind model.ContentKind) ModelContentKind {
	switch kind {
	case model.ContentText:
		return ModelContentKindText
	case model.ContentRefusal:
		return ModelContentKindRefusal
	case model.ContentReasoning:
		return ModelContentKindReasoning
	case model.ContentToolCall:
		return 0
	default:
		return 0
	}
}

// ModelOutcomeText maps a terminal model outcome to one stable UI value.
func ModelOutcomeText(outcome model.Outcome) string {
	switch outcome {
	case model.OutcomeStop:
		return OutcomeTextStop
	case model.OutcomeToolUse:
		return OutcomeTextToolUse
	case model.OutcomeLength:
		return OutcomeTextLength
	case model.OutcomeAborted:
		return OutcomeTextAborted
	case model.OutcomeFailed:
		return OutcomeTextFailed
	default:
		return ""
	}
}

// ProjectSessionEntry constructs one public entry and identifies records without a conversation payload.
func ProjectSessionEntry(entry session.Entry, position int) (SessionEntry, bool, error) {
	if user, present := entry.User.Get(); present {
		return SessionEntry{
			ID:               entry.ID,
			CreatedAt:        entry.CreatedAt,
			Kind:             SessionEntryUser,
			User:             mo.Some(user.Clone()),
			Model:            mo.None[ModelResponse](),
			ToolResult:       mo.None[agent.ToolResult](),
			BranchSummary:    mo.None[BranchSummary](),
			ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}
	if response, present := entry.Model.Get(); present {
		mapped, err := ProjectModelResponse(response, false)
		if err != nil {
			return SessionEntry{}, false, fmt.Errorf("map restored session entry %d: %w", position, err)
		}
		return SessionEntry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: SessionEntryModel,
			User: mo.None[model.Message](), Model: mo.Some(mapped), ToolResult: mo.None[agent.ToolResult](),
			BranchSummary: mo.None[BranchSummary](), ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}
	if result, present := entry.ToolResult.Get(); present {
		return SessionEntry{
			ID:        entry.ID,
			CreatedAt: entry.CreatedAt,
			Kind:      SessionEntryToolResult,
			User:      mo.None[model.Message](),
			Model:     mo.None[ModelResponse](),
			ToolResult: mo.Some(
				result.Clone(),
			),
			BranchSummary:    mo.None[BranchSummary](),
			ExtensionMessage: mo.None[ExtensionMessage](),
		}, true, nil
	}
	if message, present := entry.ExtensionMessage.Get(); present {
		return SessionEntry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: SessionEntryExtensionMessage,
			User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
			ToolResult: mo.None[agent.ToolResult](), BranchSummary: mo.None[BranchSummary](),
			ExtensionMessage: mo.Some(ExtensionMessage{
				ExtensionID: message.ExtensionID, EntryType: message.EntryType,
				Text: message.Text, Visibility: message.Visibility,
			}),
		}, true, nil
	}
	if summary, present := entry.BranchSummary.Get(); present {
		return SessionEntry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: SessionEntryBranchSummary,
			User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
			ToolResult: mo.None[agent.ToolResult](),
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
		ToolResult: mo.None[agent.ToolResult](), Extension: mo.None[ExtensionEntry](),
		BranchSummary: mo.None[BranchSummary](), ExtensionMessage: mo.None[ExtensionMessage](),
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
	mapped.ToolResult = public.ToolResult
	mapped.BranchSummary = public.BranchSummary
	mapped.ExtensionMessage = public.ExtensionMessage
	switch public.Kind {
	case SessionEntryUser:
		mapped.Kind = SessionTreeEntryUser
	case SessionEntryModel:
		mapped.Kind = SessionTreeEntryModel
	case SessionEntryToolResult:
		mapped.Kind = SessionTreeEntryToolResult
	case SessionEntryBranchSummary:
		mapped.Kind = SessionTreeEntryBranchSummary
	case SessionEntryExtensionMessage:
		mapped.Kind = SessionTreeEntryExtensionMessage
	default:
		return SessionTreeEntry{}, fmt.Errorf("unknown tree entry payload %d", public.Kind)
	}
	return mapped, nil
}
