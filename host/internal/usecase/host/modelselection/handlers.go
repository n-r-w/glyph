package modelselection

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

const (
	// IssueCodeHandlerError identifies an ordinary handler error.
	IssueCodeHandlerError = "HANDLER_ERROR"
	// IssueCodeInvalidHandlerAction identifies a malformed handler action.
	IssueCodeInvalidHandlerAction = "INVALID_HANDLER_ACTION"
	// invalidHandlerActionText describes an action that cannot be applied.
	invalidHandlerActionText = "extension selection handler returned an invalid action"
)

// HandlerKind identifies one active-selection extension point.
type HandlerKind uint8

const (
	// HandlerKindModel transforms a model-selection request.
	HandlerKindModel HandlerKind = iota + 1
	// HandlerKindReasoning transforms a reasoning-selection request.
	HandlerKindReasoning
)

// Handler identifies one registered extension handler.
type Handler struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// HandlerID identifies the handler within its extension.
	HandlerID string
}

// registeredHandler retains the selection kind for one accepted handler.
type registeredHandler struct {
	// Handler identifies the extension and local declaration.
	Handler Handler
	// Kind identifies the request group handled by the declaration.
	Kind HandlerKind
}

// HandlerInvocation contains immutable original and composed current targets.
type HandlerInvocation struct {
	// Context is the trusted runtime and active-session binding.
	Context extension.Context
	// Kind identifies the requested selection operation.
	Kind HandlerKind
	// Original is the immutable complete starting target.
	Original model.Selection
	// Current is the complete target left by preceding handlers.
	Current model.Selection
}

// HandlerActionKind identifies the one action returned by a selection handler.
type HandlerActionKind uint8

const (
	// HandlerActionPreserve keeps the current target.
	HandlerActionPreserve HandlerActionKind = iota + 1
	// HandlerActionReplace replaces the complete current target.
	HandlerActionReplace
	// HandlerActionReject stops selection before commit.
	HandlerActionReject
)

// HandlerAction contains a decoded selection handler action before policy validation.
type HandlerAction struct {
	// Kind identifies preserve, replace, or reject.
	Kind HandlerActionKind
	// Replacement contains the complete replacement target for replace.
	Replacement model.Selection
	// Rejection contains complete nonempty rejection text for reject.
	Rejection string
}

// Issue is one ordered nonfatal selection-handler diagnostic.
type Issue struct {
	// ExtensionID identifies the extension that produced the issue.
	ExtensionID string
	// HandlerID identifies the handler that produced the issue.
	HandlerID string
	// Code identifies the stable diagnostic category.
	Code string
	// Err preserves the complete handler or validation cause.
	Err error
}

// compose runs one immutable matching handler snapshot in registration order.
func (s *Service) compose(
	ctx context.Context,
	kind HandlerKind,
	original model.Selection,
) (model.Selection, []Issue, error) {
	handlers, runtime, contexts, delivery := s.handlersFor(kind)
	current := original
	issues := make([]Issue, 0)
	for _, handler := range handlers {
		if err := ctx.Err(); err != nil {
			return model.Selection{}, issues, err
		}
		binding, issueErr := contexts.IssueContext(handler.ExtensionID)
		if issueErr != nil {
			if !runtime.HandlerRuntimeAvailable(handler.ExtensionID) {
				return model.Selection{}, issues, unavailableError(handler, issueErr)
			}
			return model.Selection{}, issues, &SelectionError{
				Code:  ErrorCodeInternal,
				cause: fmt.Errorf("issue selection context for extension %q: %w", handler.ExtensionID, issueErr),
			}
		}
		action, unavailable, handleErr := runtime.HandleSelection(
			ctx,
			handler.ExtensionID,
			handler.HandlerID,
			HandlerInvocation{
				Context: binding, Kind: kind, Original: original, Current: current,
			},
		)
		if unavailable {
			return model.Selection{}, issues, unavailableError(handler, errors.Join(handleErr, ctx.Err()))
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return model.Selection{}, issues, errors.Join(ctxErr, handleErr)
		}
		if handleErr != nil {
			if err := s.reportIssue(ctx, delivery, &issues, handler, IssueCodeHandlerError, handleErr); err != nil {
				return model.Selection{}, issues, err
			}
			continue
		}
		next, rejected, actionErr := applyAction(current, action)
		if actionErr != nil {
			if err := s.reportIssue(
				ctx,
				delivery,
				&issues,
				handler,
				IssueCodeInvalidHandlerAction,
				actionErr,
			); err != nil {
				return model.Selection{}, issues, err
			}
			continue
		}
		if rejected {
			return model.Selection{}, issues, &SelectionError{
				Code: ErrorCodeExtensionRejected,
				cause: fmt.Errorf(
					"extension %q handler %q rejected model selection: %s",
					handler.ExtensionID,
					handler.HandlerID,
					action.Rejection,
				),
			}
		}
		current = next
	}
	return current, issues, nil
}

// handlersFor snapshots matching handlers and excludes runtimes already unavailable.
func (s *Service) handlersFor(kind HandlerKind) ([]Handler, Runtime, ContextIssuer, IssueDelivery) {
	s.mutex.Lock()
	registered := slices.Clone(s.handlers)
	runtime := s.runtime
	contexts := s.contexts
	issues := s.issues
	s.mutex.Unlock()
	if len(registered) == 0 {
		return nil, runtime, contexts, issues
	}
	result := make([]Handler, 0, len(registered))
	availability := make(map[string]bool)
	for _, candidate := range registered {
		if candidate.Kind != kind {
			continue
		}
		available, checked := availability[candidate.Handler.ExtensionID]
		if !checked {
			available = runtime != nil && runtime.HandlerRuntimeAvailable(candidate.Handler.ExtensionID)
			availability[candidate.Handler.ExtensionID] = available
		}
		if available {
			result = append(result, candidate.Handler)
		}
	}
	return result, runtime, contexts, issues
}

// reportIssue publishes a nonfatal issue before retaining it for completion diagnostics.
func (s *Service) reportIssue(
	ctx context.Context,
	delivery IssueDelivery,
	issues *[]Issue,
	handler Handler,
	code string,
	cause error,
) error {
	issue := Issue{ExtensionID: handler.ExtensionID, HandlerID: handler.HandlerID, Code: code, Err: cause}
	if delivery == nil {
		return &SelectionError{
			Code:  ErrorCodeInternal,
			cause: errors.Join(cause, errors.New("selection issue delivery is not bound")),
		}
	}
	if deliveryErr := delivery.DeliverSelectionIssue(ctx, issue); deliveryErr != nil {
		return &SelectionError{
			Code:  ErrorCodeInternal,
			cause: errors.Join(cause, fmt.Errorf("deliver selection handler issue: %w", deliveryErr)),
		}
	}
	*issues = append(*issues, issue)
	return nil
}

// applyAction validates one complete action before changing current state.
func applyAction(current model.Selection, action HandlerAction) (model.Selection, bool, error) {
	switch action.Kind {
	case HandlerActionPreserve:
		if action.Replacement != (model.Selection{}) || action.Rejection != "" {
			return current, false, errors.New(invalidHandlerActionText)
		}
		return current, false, nil
	case HandlerActionReplace:
		if action.Replacement.Provider == "" || action.Replacement.Model == "" ||
			action.Replacement.ReasoningChoice == "" || action.Rejection != "" {
			return current, false, errors.New(invalidHandlerActionText)
		}
		return action.Replacement, false, nil
	case HandlerActionReject:
		if action.Replacement != (model.Selection{}) || strings.TrimSpace(action.Rejection) == "" {
			return current, false, errors.New(invalidHandlerActionText)
		}
		return current, true, nil
	default:
		return current, false, errors.New(invalidHandlerActionText)
	}
}

// unavailableError classifies selected runtime loss while retaining its cause.
func unavailableError(handler Handler, cause error) error {
	if cause == nil {
		cause = errors.New("extension runtime became unavailable")
	}
	return &SelectionError{Code: ErrorCodeExtensionUnavailable, cause: fmt.Errorf(
		"extension %q handler %q is unavailable: %w", handler.ExtensionID, handler.HandlerID, cause,
	)}
}
