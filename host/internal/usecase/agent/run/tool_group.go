package run

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// callWork retains one call's finalized value and terminal causes until its group joins.
type callWork struct {
	// result exists only after the call's finalizer succeeds.
	result mo.Option[agent.ToolResult]
	// outcome is zero for an ordinary successful or tool-error result.
	outcome agent.RunOutcome
	// err retains execution, finalization and delivery causes.
	err error
}

// executeGroup cancels siblings on terminal failures and joins every worker before returning model-ordered work.
func (s *Service) executeGroup(
	ctx context.Context,
	runID string,
	calls []model.ToolCall,
) ([]callWork, agent.RunOutcome, error) {
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make([]callWork, len(calls))
	var workers sync.WaitGroup
	for index, call := range calls {
		workers.Go(func() { work[index] = s.executeCall(groupCtx, runID, call, cancel) })
	}
	workers.Wait()
	var outcome agent.RunOutcome
	var groupErr error
	for _, completed := range work {
		if completed.outcome == agent.RunOutcomeFailed || outcome == 0 {
			outcome = completed.outcome
		}
		groupErr = errors.Join(groupErr, completed.err)
	}
	return work, outcome, groupErr
}

// executeCall finalizes and emits execution events in the worker that owns the call.
func (s *Service) executeCall(
	ctx context.Context,
	runID string,
	call model.ToolCall,
	cancel context.CancelFunc,
) callWork {
	work := callWork{result: mo.None[agent.ToolResult](), outcome: 0, err: nil}
	if err := ctx.Err(); err != nil {
		work.outcome = agent.RunOutcomeAborted
		work.err = err
		return work
	}
	start := newEvent(agent.EventToolExecutionStart, runID)
	start.ToolCall = mo.Some(call)
	if err := s.deliver(ctx, start); err != nil {
		cancel()
		work.outcome = agent.RunOutcomeFailed
		work.err = err
		return work
	}
	var progressErr error
	var progressMutex sync.Mutex
	result, executeErr := s.tools.Execute(ctx, call, func(progress tool.Progress) error {
		event := newEvent(agent.EventToolExecutionUpdate, runID)
		event.ToolCall = mo.Some(call)
		event.Progress = mo.Some(progress)
		err := s.deliver(ctx, event)
		if err != nil {
			progressMutex.Lock()
			if progressErr == nil {
				progressErr = err
			} else {
				progressErr = composeErrorWithCause(progressErr, err)
			}
			progressMutex.Unlock()
			cancel()
		}
		return err
	})
	if outcome, terminal := classifyToolFailure(ctx, executeErr, progressErr).Get(); terminal {
		work.outcome = outcome
		cancel()
	}
	if executeErr != nil {
		result = agent.ToolResult{
			Terminate: false,
			CallID:    call.ID, ToolName: call.Name, Contents: tool.TextContents(executeErr.Error()), IsError: true,
			ErrorCategory: mo.Some(agent.ToolErrorFailure),
		}
	}
	result.CallID = call.ID
	result.ToolName = call.Name
	work.err = composeErrorWithCause(executeErr, progressErr)
	result, finalizeErr := s.finalizer.FinalizeToolResult(ctx, result)
	if finalizeErr != nil {
		work.err = errors.Join(work.err, finalizeErr)
		work.outcome = agent.RunOutcomeFailed
		if ctx.Err() != nil && isPureCancellation(work.err) {
			work.outcome = agent.RunOutcomeAborted
		}
		cancel()
		return work
	}
	work.result = mo.Some(result)
	end := newEvent(agent.EventToolExecutionEnd, runID)
	end.ToolCall = mo.Some(call)
	end.ToolResult = mo.Some(result)
	if err := s.deliver(context.WithoutCancel(ctx), end); err != nil {
		cancel()
		work.outcome = agent.RunOutcomeFailed
		work.err = errors.Join(work.err, err)
	}
	return work
}
