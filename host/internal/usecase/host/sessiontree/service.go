package sessiontree

import (
	"context"
	"errors"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// Service coordinates navigation preparation, branch summarization, and atomic session commit.
type Service struct {
	// active supplies the immutable preparation snapshot and owns the commit.
	active ActiveSession
	// models snapshots active selection and executes configured-model completion.
	models ModelCompleter
}

var _ sessioncontrol.Navigator = (*Service)(nil)

// New creates an internal session-tree navigation service.
func New(active ActiveSession, models ModelCompleter) *Service {
	return &Service{active: active, models: models}
}

// NavigateTree moves the active leaf to the destination selected by one request.
func (s *Service) NavigateTree(
	ctx context.Context,
	request sessionnavigation.Request,
) (sessionnavigation.Result, error) {
	if err := ctx.Err(); err != nil {
		return sessionnavigation.Result{}, err
	}
	if err := validateRequest(request); err != nil {
		return sessionnavigation.Result{}, err
	}
	tree := s.active.Tree()
	expectedActiveLeafID := tree.ActiveLeafID()
	preparation, err := tree.NavigationPreparation(request.TargetEntryID)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	summary := mo.None[BranchSummaryDraft]()
	if request.SummaryMode != sessionnavigation.SummaryModeNoSummary && len(preparation.AbandonedPath) != 0 {
		selection := s.models.Selection()
		generated, summaryErr := s.summarize(ctx, selection, preparation, request.CustomFocus)
		if summaryErr != nil {
			return sessionnavigation.Result{}, summaryErr
		}
		summary = mo.Some(generated)
	}
	if cancellationErr := ctx.Err(); cancellationErr != nil {
		return sessionnavigation.Result{}, cancellationErr
	}
	committed, err := s.active.CommitNavigation(ctx, CommitCommand{
		ExpectedActiveLeafID: expectedActiveLeafID,
		DestinationID:        preparation.DestinationID,
		BranchSummary:        summary,
	})
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	return sessionnavigation.Result{
		Tree: committed, ActiveLeafID: committed.ActiveLeafID(), ActiveBranch: committed.ActiveBranch(),
		NextInput: preparation.NextInput,
	}, nil
}

// validateRequest enforces the closed summary-mode and custom-focus contract.
func validateRequest(request sessionnavigation.Request) error {
	focus := strings.TrimSpace(request.CustomFocus.OrEmpty())
	if request.TargetEntryID == "" {
		return errors.New("tree navigation target is required")
	}
	switch request.SummaryMode {
	case sessionnavigation.SummaryModeNoSummary, sessionnavigation.SummaryModeSummarize:
		if focus != "" {
			return errors.New("custom focus is not allowed for this summary mode")
		}
	case sessionnavigation.SummaryModeSummarizeWithCustomPrompt:
		if focus == "" {
			return errors.New("custom focus is required")
		}
	default:
		return errors.New("summary mode is invalid")
	}
	return nil
}
