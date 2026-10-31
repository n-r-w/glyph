package run

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// skipLengthCalls records an error result for each tool call of a length-limited response instead of executing it.
func (s *Service) skipLengthCalls(
	ctx context.Context,
	runID string,
	response model.Response,
) (turnResult, bool, error) {
	calls := response.ToolCalls()
	if len(calls) == 0 {
		return s.endTurn(ctx, runID, response, nil, agent.RunOutcomeCompleted, "", nil)
	}
	results := make([]agent.ToolResult, 0, len(calls))
	for _, call := range calls {
		result, finalizeErr := s.finalizer.FinalizeToolResult(ctx, agent.ToolResult{
			CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(lengthCallMessage), IsError: true,
		})
		if finalizeErr != nil {
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(visibleErrorMessage(finalizeErr)),
			}, false, finalizeErr
		}
		if err := s.appendToolResult(context.WithoutCancel(ctx), result); err != nil {
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(err.Error()),
			}, false, err
		}
		results = append(results, result)
		toolResult := newEvent(agent.EventToolResult, runID)
		toolResult.ToolResult = mo.Some(result)
		if err := s.deliver(context.WithoutCancel(ctx), toolResult); err != nil {
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(err.Error()),
			}, false, err
		}
	}
	return s.endTurn(ctx, runID, response, results, 0, "", nil)
}

// executeCalls runs finalized tool calls sequentially in model order.
func (s *Service) executeCalls(
	ctx context.Context,
	runID string,
	response model.Response,
) (turnResult, bool, error) {
	results := make([]agent.ToolResult, 0)
	for _, call := range response.ToolCalls() {
		if err := ctx.Err(); err != nil {
			return s.endTurn(ctx, runID, response, results, agent.RunOutcomeAborted, abortedModelMessage, err)
		}
		toolStart := newEvent(agent.EventToolExecutionStart, runID)
		toolStart.ToolCall = mo.Some(call)
		if err := s.deliver(ctx, toolStart); err != nil {
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(err.Error()),
			}, false, err
		}
		var progressDeliveryErr error
		result, executeErr := s.tools.Execute(ctx, call, func(progress tool.Progress) error {
			toolUpdate := newEvent(agent.EventToolExecutionUpdate, runID)
			toolUpdate.ToolCall = mo.Some(call)
			toolUpdate.Progress = mo.Some(progress)
			deliveryErr := s.deliver(ctx, toolUpdate)
			if deliveryErr != nil && progressDeliveryErr == nil {
				progressDeliveryErr = deliveryErr
			}
			return deliveryErr
		})
		if executeErr != nil {
			result = agent.ToolResult{
				CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(executeErr.Error()), IsError: true,
			}
		}
		result.CallID = call.ID
		result.ToolName = call.Name
		priorErr := composeErrorWithCause(executeErr, progressDeliveryErr)
		result, finalizeErr := s.finalizer.FinalizeToolResult(ctx, result)
		if finalizeErr != nil {
			combinedErr := errors.Join(finalizeErr, priorErr)
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(visibleErrorMessage(combinedErr)),
			}, false, combinedErr
		}
		if err := s.appendToolResult(context.WithoutCancel(ctx), result); err != nil {
			combinedErr := errors.Join(err, priorErr)
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(visibleErrorMessage(combinedErr)),
			}, false, combinedErr
		}
		results = append(results, result)
		toolEnd := newEvent(agent.EventToolExecutionEnd, runID)
		toolEnd.ToolCall = mo.Some(call)
		toolEnd.ToolResult = mo.Some(result)
		endErr := s.deliver(context.WithoutCancel(ctx), toolEnd)
		toolResult := newEvent(agent.EventToolResult, runID)
		toolResult.ToolResult = mo.Some(result)
		resultErr := s.deliver(context.WithoutCancel(ctx), toolResult)
		if endErr != nil || resultErr != nil {
			combinedErr := errors.Join(priorErr, endErr, resultErr)
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(visibleErrorMessage(combinedErr)),
			}, false, combinedErr
		}
		if outcome, terminal := classifyToolFailure(ctx, executeErr, progressDeliveryErr).Get(); terminal {
			return s.endTurn(
				ctx, runID, response, results, outcome, visibleErrorMessage(priorErr), priorErr,
			)
		}
	}
	return s.endTurn(ctx, runID, response, results, 0, "", nil)
}

// composeErrorWithCause adds a cause only when the primary error does not already contain it.
func composeErrorWithCause(primaryErr, causeErr error) error {
	if causeErr == nil || errors.Is(primaryErr, causeErr) {
		return primaryErr
	}
	return errors.Join(primaryErr, causeErr)
}
