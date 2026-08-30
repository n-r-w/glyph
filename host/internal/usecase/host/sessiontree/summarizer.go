package sessiontree

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

const (
	// selectionCodeNotFound identifies an unavailable configured provider and model pair.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoningUnsupported identifies an unsupported configured reasoning choice.
	selectionCodeReasoningUnsupported = "reasoning_unsupported"
	// selectionCodeCredentialUnavailable identifies unresolved configured credentials.
	selectionCodeCredentialUnavailable = "credential_unavailable" //nolint:gosec // Public error code, not a secret.
)

// selectionFailure exposes provider-catalog failure classification without coupling to its implementation.
type selectionFailure interface {
	error
	// SelectionCode returns the stable configured-selection failure code.
	SelectionCode() string
}

// summarize executes one configured model request for the exact abandoned path.
func (s *Service) summarize(
	ctx context.Context,
	selection model.Selection,
	preparation session.NavigationPreparation,
	customFocus mo.Option[string],
) (BranchSummaryDraft, error) {
	// conversation contains only approved source values in the summary-specific representation.
	conversation, err := serializeBranchSummaryConversation(preparation.AbandonedPath)
	if err != nil {
		return BranchSummaryDraft{}, fmt.Errorf("prepare branch summary conversation: %w", err)
	}
	// task contains one bounded source conversation and optional escaped caller focus.
	task, err := renderBranchSummaryTask(conversation, customFocus)
	if err != nil {
		return BranchSummaryDraft{}, fmt.Errorf("prepare branch summary task: %w", err)
	}
	// history keeps source records out of model roles by sending one serialized user input.
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(task)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	response, err := s.models.CompleteConfigured(
		ctx,
		selection,
		branchSummarySystemText,
		history,
	)
	if err != nil {
		return BranchSummaryDraft{}, classifyCompletionError(ctx, err)
	}
	summary, usage, err := validateSummaryResponse(response)
	if err != nil {
		return BranchSummaryDraft{}, err
	}
	return BranchSummaryDraft{
		Summary: summary, FirstEntryID: preparation.AbandonedPath[0].ID,
		LastEntryID:      preparation.AbandonedPath[len(preparation.AbandonedPath)-1].ID,
		CommonAncestorID: preparation.CommonAncestorID, Selection: selection, Usage: usage,
	}, nil
}

// classifyCompletionError maps configured-selection failures and preserves context cancellation.
func classifyCompletionError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if classified, ok := errors.AsType[selectionFailure](err); ok {
		switch classified.SelectionCode() {
		case selectionCodeNotFound, selectionCodeReasoningUnsupported:
			return fmt.Errorf("%w: %w", sessionnavigation.ErrModelUnavailable, err)
		case selectionCodeCredentialUnavailable:
			return fmt.Errorf("%w: %w", sessionnavigation.ErrCredentialUnavailable, err)
		}
	}
	return fmt.Errorf("%w: %w", sessionnavigation.ErrModelFailed, err)
}

// validateSummaryResponse accepts only terminal visible text and validates optional normalized usage.
func validateSummaryResponse(response model.Response) (string, mo.Option[session.TokenUsage], error) {
	outcome, present := response.Outcome.Get()
	if !present || outcome != model.OutcomeStop && outcome != model.OutcomeLength {
		return "", mo.None[session.TokenUsage](), sessionnavigation.ErrModelFailed
	}
	if err := response.ValidateTerminalContent(); err != nil {
		return "", mo.None[session.TokenUsage](), fmt.Errorf("%w: %w", sessionnavigation.ErrModelFailed, err)
	}
	for index := range response.Content {
		if response.Content[index].Kind == model.ContentToolCall {
			return "", mo.None[session.TokenUsage](), sessionnavigation.ErrModelFailed
		}
	}
	summary := response.Text()
	if strings.TrimSpace(summary) == "" {
		return "", mo.None[session.TokenUsage](), sessionnavigation.ErrModelFailed
	}
	usage := mo.None[session.TokenUsage]()
	if reported, available := response.Usage.Get(); available {
		value := session.TokenUsage{
			InputTokens: reported.InputTokens, OutputTokens: reported.OutputTokens,
			CacheReadTokens: reported.CachedInputTokens, CacheWriteTokens: reported.CacheWriteTokens,
			ReasoningTokens: reported.ReasoningTokens, TotalTokens: reported.TotalTokens,
		}
		if !value.Valid() {
			return "", mo.None[session.TokenUsage](), sessionnavigation.ErrModelFailed
		}
		usage = mo.Some(value)
	}
	return summary, usage, nil
}
