package sessiontree

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// invalidHandlerActionMessage describes an action that cannot be applied to handler state.
const invalidHandlerActionMessage = "extension handler returned an invalid action"

// runRequestHandlers applies one immutable handler snapshot in registration order.
func (s *Service) runRequestHandlers(
	ctx context.Context,
	tree session.Tree,
	original HandlerNavigationState,
) (
	HandlerNavigationState,
	mo.Option[HandlerBranchSummaryResult],
	[]navigationIssue,
	bool,
	error,
) {
	current := cloneHandlerState(original)
	currentResult := mo.None[HandlerBranchSummaryResult]()
	var issues []navigationIssue
	for _, handler := range s.handlersFor(HandlerKindRequest) {
		action, err := s.invokeRequestHandler(ctx, handler, RequestHandlerInvocation{
			Original: cloneHandlerState(original), Current: cloneHandlerState(current), CurrentResult: currentResult,
		})
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return HandlerNavigationState{}, mo.None[HandlerBranchSummaryResult](), issues, false, errors.Join(
					contextErr,
					err,
				)
			}
			issues = append(
				issues,
				operationIssue(navigationIssueHandlerError, handler, err),
			)
			continue
		}
		candidate, result, canceled, validationErr := applyRequestHandlerAction(tree, current, currentResult, action)
		if validationErr != nil {
			issues = append(issues, operationIssue(
				navigationIssueInvalidHandlerAction,
				handler,
				validationErr,
			))
			continue
		}
		if canceled {
			return current, currentResult, issues, true, nil
		}
		current, currentResult = candidate, result
	}
	return current, currentResult, issues, false, nil
}

// applyRequestHandlerAction validates the complete action before applying any state change.
func applyRequestHandlerAction(
	tree session.Tree,
	current HandlerNavigationState,
	currentResult mo.Option[HandlerBranchSummaryResult],
	action RequestHandlerAction,
) (HandlerNavigationState, mo.Option[HandlerBranchSummaryResult], bool, error) {
	if action.Cancel {
		valid := action.RequestAction == 0 && action.Request.IsNone() && action.ResultAction == 0 &&
			action.Result.IsNone()
		if !valid {
			return current, currentResult, false, errors.New(invalidHandlerActionMessage)
		}
		return current, currentResult, true, nil
	}
	candidate, err := applyNavigationRequestAction(tree, current, action)
	if err != nil {
		return current, currentResult, false, err
	}
	result, resultValid := applyOptionalResultAction(currentResult, action)
	if !resultValid {
		return current, currentResult, false, errors.New(invalidHandlerActionMessage)
	}
	return candidate, result, false, nil
}

// applyNavigationRequestAction validates and applies only the request part of one action.
func applyNavigationRequestAction(
	tree session.Tree,
	current HandlerNavigationState,
	action RequestHandlerAction,
) (HandlerNavigationState, error) {
	candidate := cloneHandlerState(current)
	switch action.RequestAction {
	case RequestActionPreserve:
		if action.Request.IsSome() {
			return current, errors.New(invalidHandlerActionMessage)
		}
		return candidate, nil
	case RequestActionReplace:
		replacement, present := action.Request.Get()
		if !present {
			return current, errors.New(invalidHandlerActionMessage)
		}
		if err := validateRequest(replacement.Navigation); err != nil {
			return current, err
		}
		preparation, err := tree.NavigationPreparation(replacement.Navigation.TargetEntryID)
		if err != nil {
			return current, err
		}
		candidate.Request = replacement
		candidate.Preparation = projectPreparation(preparation)
		return candidate, nil
	default:
		return current, errors.New(invalidHandlerActionMessage)
	}
}

// applyOptionalResultAction validates and applies the optional result part of one request action.
func applyOptionalResultAction(
	current mo.Option[HandlerBranchSummaryResult],
	action RequestHandlerAction,
) (mo.Option[HandlerBranchSummaryResult], bool) {
	switch action.ResultAction {
	case ResultActionPreserve:
		return current, action.Result.IsNone()
	case ResultActionReplace:
		replacement, present := action.Result.Get()
		if !present {
			return current, false
		}
		return mo.Some(replacement), true
	case ResultActionClear:
		return mo.None[HandlerBranchSummaryResult](), action.Result.IsNone()
	default:
		return current, false
	}
}

// runResultHandlers applies ordered result transformations while preserving the entering result.
func (s *Service) runResultHandlers(
	ctx context.Context,
	original HandlerNavigationState,
	current HandlerNavigationState,
	result mo.Option[HandlerBranchSummaryResult],
	issues []navigationIssue,
) (mo.Option[HandlerBranchSummaryResult], []navigationIssue, bool, error) {
	originalResult, present := result.Get()
	if !present {
		return result, issues, false, nil
	}
	currentResult := originalResult
	for _, handler := range s.handlersFor(HandlerKindResult) {
		action, err := s.invokeResultHandler(ctx, handler, ResultHandlerInvocation{
			Original: cloneHandlerState(original), Current: cloneHandlerState(current),
			OriginalResult: originalResult, CurrentResult: currentResult,
		})
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return mo.None[HandlerBranchSummaryResult](), issues, false, errors.Join(contextErr, err)
			}
			issues = append(
				issues,
				operationIssue(navigationIssueHandlerError, handler, err),
			)
			continue
		}
		candidate, canceled, valid := applyResultHandlerAction(currentResult, action)
		if !valid {
			issues = append(issues, operationIssue(
				navigationIssueInvalidHandlerAction,
				handler,
				errors.New(invalidHandlerActionMessage),
			))
			continue
		}
		if canceled {
			return mo.Some(currentResult), issues, true, nil
		}
		currentResult = candidate
	}
	return mo.Some(currentResult), issues, false, nil
}

// applyResultHandlerAction validates one result action before replacing current output.
func applyResultHandlerAction(
	current HandlerBranchSummaryResult,
	action ResultHandlerAction,
) (HandlerBranchSummaryResult, bool, bool) {
	if action.Cancel {
		valid := action.ResultAction == 0 && action.Result.IsNone()
		return current, valid, valid
	}
	switch action.ResultAction {
	case ResultActionPreserve:
		return current, false, action.Result.IsNone()
	case ResultActionReplace:
		replacement, present := action.Result.Get()
		return replacement, false, present
	case ResultActionClear:
		return current, false, false
	default:
		return current, false, false
	}
}

// runObservers invokes every snapshotted observer after commit and preserves received failures.
func (s *Service) runObservers(
	ctx context.Context,
	current HandlerNavigationState,
	committed session.Tree,
	summaryCreated bool,
	issues []navigationIssue,
) []navigationIssue {
	// A committed navigation must reach observers even when the caller cancels after persistence.
	observerContext := context.WithoutCancel(ctx)
	invocation := TreeObserverInvocation{
		SessionID: current.SessionID, TargetEntryID: current.Request.Navigation.TargetEntryID,
		PrecedingActiveLeafID:   current.PrecedingActiveLeafID,
		NavigationDestinationID: current.Preparation.DestinationID,
		CommittedActiveLeafID:   committed.ActiveLeafID(),
		CreatedSummary:          committedSummary(committed, summaryCreated),
	}
	for _, handler := range s.handlersFor(HandlerKindObserver) {
		if err := s.invokeObserver(observerContext, handler, invocation); err != nil {
			issues = append(
				issues,
				operationIssue(navigationIssueObserverError, handler, err),
			)
		}
	}
	return issues
}

// committedSummary returns the summary entry created by a successful navigation commit.
func committedSummary(tree session.Tree, expected bool) mo.Option[session.Entry] {
	if !expected {
		return mo.None[session.Entry]()
	}
	branch := tree.ActiveBranch()
	if len(branch) == 0 || branch[len(branch)-1].BranchSummary.IsNone() {
		return mo.None[session.Entry]()
	}
	return mo.Some(branch[len(branch)-1].Clone())
}

// operationIssue attaches handler identity and a category without changing the received message.
func operationIssue(
	code navigationIssueCode,
	handler Handler,
	cause error,
) navigationIssue {
	return navigationIssue{
		Code: code, ExtensionID: handler.ExtensionID, HandlerID: handler.HandlerID, Message: cause.Error(),
		source: fmt.Errorf("extension %q handler %q: %w", handler.ExtensionID, handler.HandlerID, cause),
	}
}

// projectPreparation removes opaque extension payloads before handler dispatch.
func projectPreparation(preparation session.NavigationPreparation) session.NavigationPreparation {
	preparation.AbandonedPath = cloneEntries(preparation.AbandonedPath)
	for index := range preparation.AbandonedPath {
		extension, present := preparation.AbandonedPath[index].Extension.Get()
		if !present {
			continue
		}
		extension.Data = nil
		preparation.AbandonedPath[index].Extension = mo.Some(extension)
	}
	return preparation
}

// cloneHandlerState protects immutable preparation entries from in-process mutation.
func cloneHandlerState(state HandlerNavigationState) HandlerNavigationState {
	state.Preparation.AbandonedPath = cloneEntries(state.Preparation.AbandonedPath)
	return state
}

// cloneEntries returns independent session entries in the same order.
func cloneEntries(entries []session.Entry) []session.Entry {
	cloned := make([]session.Entry, len(entries))
	for index := range entries {
		cloned[index] = entries[index].Clone()
	}
	return cloned
}
