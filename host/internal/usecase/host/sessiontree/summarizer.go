package sessiontree

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/errtree"
)

const (
	// selectionCodeNotFound identifies an unavailable configured provider and model pair.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoningUnsupported identifies an unsupported configured reasoning choice.
	selectionCodeReasoningUnsupported = "reasoning_unsupported"
	// selectionCodeCredentialUnavailable identifies unresolved configured credentials.
	selectionCodeCredentialUnavailable = "credential_unavailable" //nolint:gosec // Public error code, not a secret.
)

// summarize executes one model request with the exact selection for the abandoned path.
func (s *Service) summarize(
	ctx context.Context,
	selection model.Selection,
	preparation session.NavigationPreparation,
	customFocus mo.Option[string],
	progress func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error,
) (BranchSummaryDraft, error) {
	// conversation contains only approved source values in the summary-specific representation.
	conversation := serializeBranchSummaryConversation(preparation.AbandonedPath)
	// userInput contains one bounded source conversation, optional escaped focus, and the embedded task.
	userInput := renderBranchSummaryUserInput(conversation, customFocus)
	// history sends exactly one provider user-role message and keeps source records out of provider roles.
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(userInput)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	response, err := s.modelRequester.RequestConfigured(
		ctx,
		selection,
		branchSummarySystemText,
		history,
		progress,
	)
	if err != nil {
		return BranchSummaryDraft{}, classifyModelRequestError(ctx, err)
	}
	summary, usage, err := validateSummaryResponse(response)
	if err != nil {
		return BranchSummaryDraft{}, err
	}
	return BranchSummaryDraft{
		Summary: summary, FirstEntryID: preparation.AbandonedPath[0].ID,
		LastEntryID:      preparation.AbandonedPath[len(preparation.AbandonedPath)-1].ID,
		CommonAncestorID: preparation.CommonAncestorID,
		Source: session.BranchSummarySource{
			ExtensionID: mo.None[string](),
			Model:       mo.Some(session.BranchSummaryModelSource{Selection: selection, Usage: usage}),
		},
	}, nil
}

// classifyModelRequestError maps acquired source failures before reducing a pure caller cancellation.
func classifyModelRequestError(ctx context.Context, err error) error {
	if classified, ok := errors.AsType[SelectionFailure](err); ok {
		switch classified.SelectionCode() {
		case selectionCodeNotFound, selectionCodeReasoningUnsupported:
			return fmt.Errorf("%w: %w", ErrModelUnavailable, err)
		case selectionCodeCredentialUnavailable:
			return fmt.Errorf("%w: %w", ErrCredentialUnavailable, err)
		}
	}
	if _, ok := errors.AsType[ModelRequestFailure](err); ok {
		return fmt.Errorf("%w: %w", ErrModelFailed, err)
	}
	if contextErr := ctx.Err(); contextErr != nil && isPureModelRequestCancellation(err) {
		return contextErr
	}
	return fmt.Errorf("%w: %w", ErrModelFailed, err)
}

// isPureModelRequestCancellation reports whether every acquired source cause is cancellation.
func isPureModelRequestCancellation(err error) bool {
	return errtree.AllLeavesMatch(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	})
}

// validateSummaryResponse accepts only terminal visible text and validates optional normalized usage.
func validateSummaryResponse(response model.Response) (string, mo.Option[session.TokenUsage], error) {
	outcome, present := response.Outcome.Get()
	if !present || outcome != model.OutcomeStop && outcome != model.OutcomeLength {
		return "", mo.None[session.TokenUsage](), ErrModelFailed
	}
	if err := response.ValidateTerminalContent(); err != nil {
		return "", mo.None[session.TokenUsage](), fmt.Errorf("%w: %w", ErrModelFailed, err)
	}
	for index := range response.Content {
		if response.Content[index].Kind == model.ContentToolCall {
			return "", mo.None[session.TokenUsage](), ErrModelFailed
		}
	}
	summary := response.Text()
	if strings.TrimSpace(summary) == "" {
		return "", mo.None[session.TokenUsage](), ErrModelFailed
	}
	usage := mo.None[session.TokenUsage]()
	if reported, available := response.Usage.Get(); available {
		value := session.TokenUsage{
			InputTokens: reported.InputTokens, OutputTokens: reported.OutputTokens,
			CacheReadTokens: reported.CachedInputTokens, CacheWriteTokens: reported.CacheWriteTokens,
			ReasoningTokens: reported.ReasoningTokens, TotalTokens: reported.TotalTokens,
		}
		if !value.Valid() {
			return "", mo.None[session.TokenUsage](), ErrModelFailed
		}
		usage = mo.Some(value)
	}
	return summary, usage, nil
}
