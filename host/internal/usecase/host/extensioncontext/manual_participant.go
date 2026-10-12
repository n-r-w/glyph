package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
)

const (
	// requestParticipantKind identifies request-handler diagnostics.
	requestParticipantKind = "request"
	// generatorParticipantKind identifies generator diagnostics.
	generatorParticipantKind = "generator"
)

// beginParticipant starts a time-limited, session-bound lifetime for a captured participant.
func (s *Service) beginParticipant(
	ctx context.Context,
	parent *compactionOperation,
	participant Participant,
	kind string,
) (childContext context.Context, child *compactionOperation, bound extension.Context, err error) {
	bound, err = s.IssueContext(participant.ExtensionID)
	if err != nil {
		return nil, nil, extension.Context{}, fmt.Errorf("issue captured %s participant context: %w", kind, err)
	}
	s.mutex.Lock()
	incarnation := s.bindings[participant.ExtensionID].incarnation
	s.mutex.Unlock()
	if bound.RuntimeInstanceID != participant.RuntimeID || bound.SessionID != parent.snapshot.Identity.ID ||
		incarnation != parent.snapshot.Identity.Incarnation {
		return nil, nil, extension.Context{}, fmt.Errorf(
			"captured %s participant runtime or session changed before invocation",
			kind,
		)
	}
	// A lifetime identifies nested work independently of the policy's authority.
	lifetimeID := rand.Text()
	childContext, cancel := context.WithTimeout(ctx, s.participantTimeout)
	child = &compactionOperation{
		authorityID: "", lifecycleID: lifetimeID, owner: PolicyOwner(participant),
		snapshot: CompactionSnapshot{}, requests: nil, generators: nil, results: nil,
		context: childContext, cancel: cancel, mutex: sync.Mutex{}, children: sync.WaitGroup{},
		closed: false, replacementRequested: false, receipt: mo.None[session.Entry](),
		causes: nil, terminalError: nil, state: mo.None[contextcompaction.PolicyState](),
	}
	s.mutex.Lock()
	s.compactionOperations[lifetimeID] = child
	s.mutex.Unlock()
	return childContext, child, bound, nil
}

// finishParticipant joins nested work and retains acquired causes in its owning policy operation.
func (s *Service) finishParticipant(
	ctx, execution context.Context,
	parent, child *compactionOperation,
	invokeErr error,
) error {
	// Observe execution before finish cancels the lifetime during normal teardown.
	executionErr := errors.Join(execution.Err(), context.Cause(execution))
	_, acquired, terminal := child.finish()
	s.mutex.Lock()
	delete(s.compactionOperations, child.lifecycleID)
	s.mutex.Unlock()
	parent.mutex.Lock()
	parent.causes = errors.Join(parent.causes, acquired)
	parent.mutex.Unlock()
	if err := errors.Join(invokeErr, executionErr, terminal, ctx.Err(), context.Cause(ctx)); err != nil {
		return errors.Join(err, acquired)
	}
	return nil
}
