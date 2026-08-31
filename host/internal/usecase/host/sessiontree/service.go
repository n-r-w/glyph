package sessiontree

import (
	"context"
	"errors"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// Service coordinates navigation preparation, branch summarization, and atomic session commit.
type Service struct {
	// active supplies the immutable preparation snapshot and owns the commit.
	active ActiveSession
	// modelRequester supplies selection state and executes model requests.
	modelRequester ModelRequester
	// handlers supplies ordered extension handlers for navigation.
	handlers HandlerRunner
}

var _ sessioncontrol.Navigator = (*Service)(nil)

// New creates an internal session-tree navigation service.
func New(active ActiveSession, modelRequester ModelRequester, handlers HandlerRunner) *Service {
	return &Service{active: active, modelRequester: modelRequester, handlers: handlers}
}

// NavigateTree composes extension handlers around one atomic navigation commit.
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
	selection := s.modelRequester.ActiveSelection()
	original := HandlerNavigationState{
		SessionID:             s.active.SessionID(),
		PrecedingActiveLeafID: expectedActiveLeafID,
		Request: HandlerNavigationRequest{
			Navigation:   request,
			SummaryModel: selection,
		},
		Preparation: projectPreparation(preparation),
	}
	current, currentResult, issues, canceled, err := s.runRequestHandlers(ctx, tree, original)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	if canceled {
		return canceledResult(issues), nil
	}

	currentResult, err = s.generateMissingSummary(ctx, current, currentResult)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	currentResult, issues, canceled, err = s.runAvailableResultHandlers(
		ctx,
		original,
		current,
		currentResult,
		issues,
	)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	if canceled {
		return canceledResult(issues), nil
	}

	preparation, summary, err := s.validateFinalState(ctx, tree, current, currentResult)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return sessionnavigation.Result{}, contextErr
	}
	committed, err := s.active.CommitNavigation(ctx, CommitCommand{
		ExpectedActiveLeafID: expectedActiveLeafID,
		DestinationID:        preparation.DestinationID,
		BranchSummary:        summary,
	})
	if err != nil {
		return sessionnavigation.Result{}, err
	}

	issues = s.runObservers(ctx, current, committed, summary.IsSome(), issues)
	return sessionnavigation.Result{
		Canceled: false, Tree: committed, ActiveLeafID: committed.ActiveLeafID(),
		ActiveBranch: committed.ActiveBranch(), NextInput: preparation.NextInput, Issues: issues,
	}, nil
}

// generateMissingSummary runs built-in behavior only when handlers left no result.
func (s *Service) generateMissingSummary(
	ctx context.Context,
	current HandlerNavigationState,
	result mo.Option[HandlerBranchSummaryResult],
) (mo.Option[HandlerBranchSummaryResult], error) {
	if current.Request.Navigation.SummaryMode == sessionnavigation.SummaryModeNoSummary ||
		len(current.Preparation.AbandonedPath) == 0 || result.IsSome() {
		return result, nil
	}
	generated, err := s.summarize(
		ctx,
		current.Request.SummaryModel,
		current.Preparation,
		current.Request.Navigation.CustomFocus,
	)
	if err != nil {
		return mo.None[HandlerBranchSummaryResult](), err
	}
	return mo.Some(summaryResultFromDraft(generated)), nil
}

// runAvailableResultHandlers skips the result extension point when no result exists.
func (s *Service) runAvailableResultHandlers(
	ctx context.Context,
	original HandlerNavigationState,
	current HandlerNavigationState,
	result mo.Option[HandlerBranchSummaryResult],
	issues []sessionnavigation.OperationIssue,
) (mo.Option[HandlerBranchSummaryResult], []sessionnavigation.OperationIssue, bool, error) {
	if result.IsNone() {
		return result, issues, false, nil
	}
	return s.runResultHandlers(ctx, original, current, result, issues)
}

// canceledResult creates a state-free cancellation outcome with preceding issues.
func canceledResult(issues []sessionnavigation.OperationIssue) sessionnavigation.Result {
	return sessionnavigation.Result{
		Canceled: true, Tree: session.Tree{}, ActiveLeafID: mo.None[string](), ActiveBranch: nil,
		NextInput: mo.None[string](), Issues: issues,
	}
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
