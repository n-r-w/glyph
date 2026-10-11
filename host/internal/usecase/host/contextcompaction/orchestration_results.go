package contextcompaction

import (
	"context"
	"fmt"

	"github.com/samber/mo"
)

// prepareCompactionResult supplies the original result for captured result handlers.
func (s *Service) prepareCompactionResult(
	ctx context.Context,
	snapshot Snapshot,
	handlers HandlerSet,
	original, current Request,
	currentResult mo.Option[Result],
) (Result, bool, error) {
	if supplied, present := currentResult.Get(); present {
		return cloneResult(supplied), false, nil
	}
	generated, err := s.generateCompaction(ctx, snapshot, handlers, original, current)
	if err != nil {
		canceled, failure := classifyExtensionInvocation(ctx, err)
		return Result{}, canceled, failure
	}
	return cloneResult(generated), false, nil
}

// composeResultHandlers applies captured result actions in registration order.
func (s *Service) composeResultHandlers(
	ctx context.Context,
	snapshot Snapshot,
	handlers []Handler,
	original, current Request,
	originalResult Result,
) (Result, bool, error) {
	const handlerFailureFormat = "compaction result handler %q: %w"

	result := cloneResult(originalResult)
	for _, handler := range handlers {
		binding, err := s.contexts.IssueContext(handler.ExtensionID)
		if err != nil {
			return result, false, extensionFailure(
				fmt.Errorf("issue context for compaction result handler %q: %w", handler.HandlerID, err),
			)
		}
		action, err := s.runtime.HandleCompactionResult(ctx, handler, binding, ResultInvocation{
			OriginalRequest: cloneRequest(
				original,
			),
			CurrentRequest: cloneRequest(current),
			OriginalResult: cloneResult(originalResult),
			CurrentResult:  cloneResult(result),
		})
		if err != nil {
			canceled, failure := classifyExtensionInvocation(
				ctx,
				fmt.Errorf(handlerFailureFormat, handler.HandlerID, err),
			)
			return result, canceled, failure
		}
		var canceled bool
		result, canceled, err = s.applyValidatedResultAction(snapshot, handler, result, action)
		if err != nil {
			return result, false, extensionFailure(
				fmt.Errorf(handlerFailureFormat, handler.HandlerID, err),
			)
		}
		if canceled {
			return result, true, context.Cause(ctx)
		}
	}
	return result, false, nil
}

// applyValidatedResultAction supplies the next result with producer and boundary validation.
func (s *Service) applyValidatedResultAction(
	snapshot Snapshot,
	handler Handler,
	result Result,
	action ResultAction,
) (Result, bool, error) {
	var err error
	if replacement, present := action.Result.Get(); !action.Preserve && !action.Cancel && present {
		err = validateProducer(handler.ExtensionID, replacement)
	}
	var canceled bool
	if err == nil {
		result, canceled, err = applyResultAction(result, action)
	}
	if err == nil && !canceled {
		err = s.validateResultState(snapshot, result)
	}
	return result, canceled, err
}
