package extensioncontext

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// ProjectPolicyReplacement supplies the validated candidate history for the owning invocation.
func (s *Service) ProjectPolicyReplacement(
	ctx context.Context,
	extensionID, runtimeID, operationID string,
	candidate session.CompactionEntry,
) ([]agent.HistoryEntry, error) {
	// operation contains the captured mutation authority; err contains any lookup failure.
	operation, err := s.findCompactionOperation(extensionID, runtimeID, operationID)
	if err != nil {
		return nil, err
	}
	// cancellationErr contains cancellation causes that prohibit this operation.
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
	// operation contains the captured mutation authority; err contains any lookup failure.
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
		// cancellationErr contains cancellation causes that prohibit this operation.
		if cancellationErr := errors.Join(
			ctx.Err(),
			context.Cause(ctx),
			operation.context.Err(),
			context.Cause(operation.context),
		); cancellationErr != nil {
			return nil, cancellationErr
		}
		return s.runtime.BeginContextCommit(extensionID, runtimeID)
	}
	// committed contains the durable receipt; err contains persistence or publication failure.
	committed, err := s.compactionSessions.CommitReplacement(
		ctx,
		operation.snapshot,
		candidate,
		guard,
		operation.recordReceipt,
	)
	operation.mutex.Lock()
	operation.causes = errors.Join(operation.causes, err)
	operation.terminalError = errors.Join(operation.terminalError, err)
	operation.mutex.Unlock()
	return committed, err
}
