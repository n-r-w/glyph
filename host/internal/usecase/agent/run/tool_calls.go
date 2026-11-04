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
			ErrorCategory: mo.None[agent.ToolErrorCategory](),
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

// executeCalls joins each contiguous concurrent group before persisting its results in model order.
func (s *Service) executeCalls(
	ctx context.Context,
	runID string,
	response model.Response,
	catalog []tool.Descriptor,
) (turnResult, bool, error) {
	concurrent := make(map[string]bool, len(catalog))
	for index := range catalog {
		descriptor := &catalog[index]
		concurrent[descriptor.Name] = descriptor.Concurrent
	}
	calls := response.ToolCalls()
	results := make([]agent.ToolResult, 0, len(calls))
	for first := 0; first < len(calls); {
		if err := ctx.Err(); err != nil {
			return s.endTurn(ctx, runID, response, results, agent.RunOutcomeAborted, abortedModelMessage, err)
		}
		end := first + 1
		if concurrent[calls[first].Name] {
			for end < len(calls) && concurrent[calls[end].Name] {
				end++
			}
		}
		completed, outcome, groupErr := s.executeGroup(ctx, runID, calls[first:end])
		for _, work := range completed {
			result, finalized := work.result.Get()
			if !finalized {
				continue
			}
			if err := s.appendToolResult(context.WithoutCancel(ctx), result); err != nil {
				groupErr = errors.Join(err, groupErr)
				return s.endTurn(
					ctx,
					runID,
					response,
					results,
					agent.RunOutcomeFailed,
					visibleErrorMessage(groupErr),
					groupErr,
				)
			}
			results = append(results, result)
			event := newEvent(agent.EventToolResult, runID)
			event.ToolResult = mo.Some(result)
			if err := s.deliver(context.WithoutCancel(ctx), event); err != nil {
				groupErr = errors.Join(groupErr, err)
				outcome = agent.RunOutcomeFailed
			}
		}
		if outcome != 0 {
			return s.endTurn(ctx, runID, response, results, outcome, visibleErrorMessage(groupErr), groupErr)
		}
		first = end
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
