package contextcompaction

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// settleCompaction completes one composed compaction operation.
func (s *Service) settleCompaction(
	ctx context.Context,
	snapshot Snapshot,
	providerRequest modelexecution.ProviderRequest,
	handlers HandlerSet,
	original, current Request,
	result Result,
) (OperationResult, error) {
	entry, err := s.validateFinal(snapshot, providerRequest, result)
	if err != nil {
		return s.fail(ctx, handlers, original, current, mo.Some(result), false, err)
	}
	if err = ctx.Err(); err != nil {
		return s.fail(ctx, handlers, original, current, mo.Some(result), false, context.Cause(ctx))
	}
	committed, commitErr := s.CommitCompaction(ctx, snapshot.Identity, snapshot.ActiveLeafID, entry)
	commitErr = joinContextCause(ctx, commitErr)
	if committed.ID == "" {
		if commitErr == nil {
			commitErr = errors.New("compaction commit returned no committed entry")
		}
		return s.fail(ctx, handlers, original, current, mo.Some(result), false, commitErr)
	}
	outcome := OutcomeInvocation{
		State: mo.Some(
			PolicyState{
				OriginalRequest: cloneRequest(original),
				CurrentRequest:  cloneRequest(current),
				Result:          mo.Some(cloneResult(result)),
			},
		),
		ParentCompactionID: mo.None[string](),
		Committed:          mo.Some(committed.Clone()),
		Canceled:           false,
		Error:              mo.None[string](),
	}
	observerErr := s.observeSuccess(context.WithoutCancel(ctx), handlers.Successes, outcome)
	terminalErr := errors.Join(internalFailure(commitErr), extensionFailure(observerErr))
	return OperationResult{Committed: mo.Some(committed.Clone()), Canceled: false},
		joinContextCause(ctx, terminalErr)
}
