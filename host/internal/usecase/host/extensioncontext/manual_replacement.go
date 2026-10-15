// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// ProjectPolicyReplacement supplies the validated candidate history for the owning invocation.
func (s *Service) ProjectPolicyReplacement(
	ctx context.Context,
	extensionID, runtimeID, operationID string,
	candidate session.CompactionEntry,
) ([]agent.HistoryEntry, error) {
	operation, err := s.findCompactionOperation(extensionID, runtimeID, operationID)
	if err != nil {
		return nil, err
	}
	if cancellationErr := errors.Join(
		ctx.Err(),
		context.Cause(ctx),
		operation.context.Err(),
		context.Cause(operation.context),
	); cancellationErr != nil {
		return nil, cancellationErr
	}
	return s.compactionSessions.ProjectReplacement(operation.snapshot, candidate)
}

// CommitPolicyReplacement supplies one durable replacement receipt to the owning invocation.
func (s *Service) CommitPolicyReplacement(
	ctx context.Context,
	extensionID, runtimeID, operationID string,
	candidate session.CompactionEntry,
) (session.Entry, error) {
	operation, err := s.findCompactionOperation(extensionID, runtimeID, operationID)
	if err != nil {
		return session.Entry{}, err
	}
	operation.mutex.Lock()
	if operation.replacementRequested {
		operation.mutex.Unlock()
		return session.Entry{}, errors.New("compaction replacement already requested")
	}
	if operation.closed || operation.context.Err() != nil {
		operation.mutex.Unlock()
		return session.Entry{}, errors.Join(
			errors.New("compaction operation has ended before replacement"),
			operation.context.Err(), context.Cause(operation.context),
		)
	}
	operation.replacementRequested = true
	operation.mutex.Unlock()
	// guard protects runtime validity at the persistence boundary.
	guard := func() (func(), error) {
		if cancellationErr := errors.Join(
			ctx.Err(),
			context.Cause(ctx),
			operation.context.Err(),
			context.Cause(operation.context),
		); cancellationErr != nil {
			return nil, cancellationErr
		}
		release, guardErr := s.runtime.BeginContextCommit(extensionID, runtimeID)
		return release, NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, guardErr)
	}
	committed, err := s.compactionSessions.CommitReplacement(
		ctx,
		operation.snapshot,
		candidate,
		guard,
		operation.recordReceipt,
	)
	operation.mutex.Lock()
	category := modelexecution.FailureCompactionFailed
	if operation.receipt.IsSome() {
		category = modelexecution.FailureInternal
	}
	err = NewPolicyOperationError(ctx, category, err)
	operation.causes = errors.Join(operation.causes, err)
	operation.terminalError = errors.Join(operation.terminalError, err)
	operation.mutex.Unlock()
	return committed, err
}
