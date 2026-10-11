package contextcompaction

import (
	"context"
	"fmt"

	"github.com/samber/mo"
)

// composeRequestHandlers applies captured request actions in registration order.
func (s *Service) composeRequestHandlers(
	ctx context.Context,
	snapshot Snapshot,
	handlers []Handler,
	original Request,
) (Request, mo.Option[Result], bool, error) {
	current := cloneRequest(original)
	currentResult := mo.None[Result]()
	for _, handler := range handlers {
		binding, err := s.contexts.IssueContext(handler.ExtensionID)
		if err != nil {
			return current, currentResult, false, extensionFailure(
				fmt.Errorf("issue context for compaction request handler %q: %w", handler.HandlerID, err),
			)
		}
		action, err := s.runtime.HandleCompactionRequest(ctx, handler, binding, RequestInvocation{
			ParentCompactionID: mo.None[string](),
			Original:           cloneRequest(original),
			Current:            cloneRequest(current),
			CurrentResult:      cloneOptionalResult(currentResult),
		})
		if err != nil {
			canceled, failure := classifyExtensionInvocation(
				ctx,
				fmt.Errorf("compaction request handler %q: %w", handler.HandlerID, err),
			)
			return current, currentResult, canceled, failure
		}
		nextRequest, nextResult, canceled, err := s.applyValidatedRequestAction(
			snapshot,
			original,
			current,
			currentResult,
			handler,
			action,
		)
		if err != nil {
			return current, currentResult, false, extensionFailure(
				fmt.Errorf("compaction request handler %q: %w", handler.HandlerID, err),
			)
		}
		if canceled {
			return current, currentResult, true, context.Cause(ctx)
		}
		current, currentResult = nextRequest, nextResult
	}
	return current, currentResult, false, nil
}

// applyValidatedRequestAction supplies the next admissible request and optional result state.
func (s *Service) applyValidatedRequestAction(
	snapshot Snapshot,
	original, current Request,
	currentResult mo.Option[Result],
	handler Handler,
	action RequestAction,
) (Request, mo.Option[Result], bool, error) {
	var err error
	if replacement, present := action.Result.Get(); action.ResultAction == ResultActionReplace && present {
		err = validateProducer(handler.ExtensionID, replacement)
	}
	nextRequest := cloneRequest(current)
	nextResult := cloneOptionalResult(currentResult)
	var canceled bool
	if err == nil {
		nextRequest, nextResult, canceled, err = applyRequestAction(current, currentResult, action)
	}
	if err == nil && !canceled {
		nextRequest, err = s.deriveReplacementEstimates(original, current, nextRequest)
	}
	if err == nil && !canceled {
		err = s.validateRequestState(snapshot, nextRequest)
	}
	if candidate, present := nextResult.Get(); err == nil && !canceled && present {
		err = s.validateResultState(snapshot, candidate)
	}
	return nextRequest, nextResult, canceled, err
}
