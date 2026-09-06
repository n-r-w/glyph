package sessiontree

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service coordinates navigation preparation, branch summarization, and atomic session commit.
type Service struct {
	// active supplies the immutable preparation snapshot and owns the commit.
	active ActiveSession
	// modelRequester supplies selection state and executes model requests.
	modelRequester ModelRequester
	// runtime supplies availability and low-level handler invocation.
	runtime Runtime
	// contexts supplies runtime-to-active-session bindings for each invocation.
	contexts ContextIssuer
	// mutex protects accepted handler registrations.
	mutex sync.RWMutex
	// handlers contains accepted handlers in startup registration order.
	handlers []registeredHandler
}

var (
	_ ui.Navigator                 = (*Service)(nil)
	_ programmatic.Navigator       = (*Service)(nil)
	_ startup.SessionTreeRegistrar = (*Service)(nil)
)

// registeredHandler identifies one accepted handler and its extension point.
type registeredHandler struct {
	// Handler identifies the owning extension and extension-local handler.
	Handler Handler
	// Kind identifies the accepted session-tree extension point.
	Kind HandlerKind
}

// New creates an internal session-tree navigation service.
func New(active ActiveSession, modelRequester ModelRequester, runtime Runtime) *Service {
	return &Service{
		active: active, modelRequester: modelRequester, runtime: runtime, contexts: nil,
		mutex: sync.RWMutex{}, handlers: nil,
	}
}

// BindModelRequester connects the actual provider catalog before navigation can start.
func (s *Service) BindModelRequester(requester ModelRequester) {
	if s.modelRequester != nil || requester == nil {
		panic("model catalog binding must be completed exactly once")
	}
	s.modelRequester = requester
}

// BindContextIssuer completes invocation composition before handler registration.
func (s *Service) BindContextIssuer(contexts ContextIssuer) { s.contexts = contexts }

// navigate composes extension handlers around one atomic navigation commit.
func (s *Service) navigate(
	ctx context.Context,
	request NavigationRequest,
	publisher func(session.Tree) error,
) (navigationResult, error) {
	if err := ctx.Err(); err != nil {
		return navigationResult{}, err
	}
	if err := validateRequest(request); err != nil {
		return navigationResult{}, err
	}

	tree := s.active.Tree()
	expectedActiveLeafID := tree.ActiveLeafID()
	preparation, err := tree.NavigationPreparation(request.TargetEntryID)
	if err != nil {
		return navigationResult{}, err
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
		return navigationResult{}, err
	}
	if canceled {
		return canceledResult(issues), nil
	}

	currentResult, err = s.generateMissingSummary(ctx, current, currentResult)
	if err != nil {
		return navigationResult{}, err
	}
	currentResult, issues, canceled, err = s.runAvailableResultHandlers(
		ctx,
		original,
		current,
		currentResult,
		issues,
	)
	if err != nil {
		return navigationResult{}, err
	}
	if canceled {
		return canceledResult(issues), nil
	}

	preparation, summary, err := validateFinalState(tree, current, currentResult)
	if err != nil {
		return navigationResult{}, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return navigationResult{}, contextErr
	}
	commit, err := s.active.CommitNavigation(ctx, CommitCommand{
		ExpectedActiveLeafID: expectedActiveLeafID,
		DestinationID:        preparation.DestinationID,
		BranchSummary:        summary,
	}, publisher)
	if err != nil && !commit.Committed {
		return navigationResult{}, err
	}
	if err != nil {
		issues = append(issues, navigationIssue{
			Code:        navigationIssueDeliveryFailed,
			ExtensionID: "",
			HandlerID:   "",
			Message:     err.Error(),
		})
	}

	issues = s.runObservers(ctx, current, commit.Tree, commit.CreatedSummary.IsSome(), issues)
	return navigationResult{
		Canceled:       false,
		DestinationID:  preparation.DestinationID,
		ActiveLeafID:   commit.Tree.ActiveLeafID(),
		CreatedSummary: commit.CreatedSummary,
		NextInput:      preparation.NextInput,
		Issues:         issues,
	}, nil
}

// generateMissingSummary runs built-in behavior only when handlers left no result.
func (s *Service) generateMissingSummary(
	ctx context.Context,
	current HandlerNavigationState,
	result mo.Option[HandlerBranchSummaryResult],
) (mo.Option[HandlerBranchSummaryResult], error) {
	if current.Request.Navigation.SummaryMode == SummaryModeNoSummary ||
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
	issues []navigationIssue,
) (mo.Option[HandlerBranchSummaryResult], []navigationIssue, bool, error) {
	if result.IsNone() {
		return result, issues, false, nil
	}
	return s.runResultHandlers(ctx, original, current, result, issues)
}

// canceledResult creates a state-free cancellation outcome with preceding issues.
func canceledResult(issues []navigationIssue) navigationResult {
	return navigationResult{
		Canceled:       true,
		DestinationID:  mo.None[string](),
		ActiveLeafID:   mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
		NextInput:      mo.None[string](),
		Issues:         issues,
	}
}

// validateRequest enforces the closed summary-mode and custom-focus contract.
func validateRequest(request NavigationRequest) error {
	focus := strings.TrimSpace(request.CustomFocus.OrEmpty())
	if request.TargetEntryID == "" {
		return errors.New("tree navigation target is required")
	}
	switch request.SummaryMode {
	case SummaryModeNoSummary, SummaryModeSummarize:
		if focus != "" {
			return errors.New("custom focus is not allowed for this summary mode")
		}
	case SummaryModeSummarizeWithCustomPrompt:
		if focus == "" {
			return errors.New("custom focus is required")
		}
	default:
		return errors.New("summary mode is invalid")
	}
	return nil
}
