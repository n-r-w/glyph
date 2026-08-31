package sessiontree

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// validateFinalState recomputes preparation and validates the exact state before commit.
func (s *Service) validateFinalState(
	ctx context.Context,
	tree session.Tree,
	current HandlerNavigationState,
	result mo.Option[HandlerBranchSummaryResult],
) (session.NavigationPreparation, mo.Option[BranchSummaryDraft], error) {
	if err := validateRequest(current.Request.Navigation); err != nil {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(err)
	}
	preparation, err := tree.NavigationPreparation(current.Request.Navigation.TargetEntryID)
	if err != nil {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(err)
	}

	summary, present := result.Get()
	if !present {
		if current.Request.Navigation.SummaryMode != sessionnavigation.SummaryModeNoSummary &&
			len(preparation.AbandonedPath) != 0 {
			return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(
				errors.New("summary result is required"),
			)
		}
		return preparation, mo.None[BranchSummaryDraft](), nil
	}
	if current.Request.Navigation.SummaryMode == sessionnavigation.SummaryModeNoSummary ||
		len(preparation.AbandonedPath) == 0 {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(
			errors.New("summary result is inconsistent with navigation mode"),
		)
	}
	if strings.TrimSpace(summary.Summary) == "" {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(
			errors.New("summary text is empty"),
		)
	}
	if usage, usagePresent := summary.Usage.Get(); usagePresent && !usage.Valid() {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(
			errors.New("summary usage is invalid"),
		)
	}
	if selectionErr := s.modelRequester.CheckAvailability(ctx, current.Request.SummaryModel); selectionErr != nil {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), classifyModelRequestError(
			ctx,
			selectionErr,
		)
	}

	draft := BranchSummaryDraft{
		Summary:          summary.Summary,
		FirstEntryID:     preparation.AbandonedPath[0].ID,
		LastEntryID:      preparation.AbandonedPath[len(preparation.AbandonedPath)-1].ID,
		CommonAncestorID: preparation.CommonAncestorID,
		Selection:        current.Request.SummaryModel,
		Usage:            summary.Usage,
	}
	boundary := session.BranchSummaryEntry{
		Summary: draft.Summary, FirstEntryID: draft.FirstEntryID, LastEntryID: draft.LastEntryID,
		Provider: draft.Selection.Provider, Model: draft.Selection.Model,
		ReasoningChoice: draft.Selection.ReasoningChoice, Usage: draft.Usage,
		EstimatedCost: mo.None[session.EstimatedCost](),
	}
	if boundaryErr := tree.ValidateSummaryBoundary(boundary); boundaryErr != nil {
		return session.NavigationPreparation{}, mo.None[BranchSummaryDraft](), invalidExtensionState(boundaryErr)
	}
	return preparation, mo.Some(draft), nil
}

// invalidExtensionState maps final handler-produced state to its public failure class.
func invalidExtensionState(err error) error {
	return fmt.Errorf("%w: %w", sessionnavigation.ErrExtensionInvalidResult, err)
}

// summaryResultFromDraft converts built-in output into result-handler state.
func summaryResultFromDraft(draft BranchSummaryDraft) HandlerBranchSummaryResult {
	return HandlerBranchSummaryResult{Summary: draft.Summary, Usage: draft.Usage}
}
