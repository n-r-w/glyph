package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// capturedConversation retains one admitted owner and its participant generations.
type capturedConversation struct {
	// service supplies session and runtime operations.
	service *Service
	// owner identifies the admitted policy generation.
	owner PolicyOwner
	// binding identifies the admitted session context.
	binding extension.Context
	// incarnation identifies the admitted session lifetime.
	incarnation uint64
	// handlers contains captured composition and outcome registrations.
	handlers contextcompaction.HandlerSet
}

// capturedConversation supplies policy preparation and completed observation to model execution.
var _ modelexecution.CapturedConversation = (*capturedConversation)(nil)

// CaptureConversation admits the optional policy owner for preparation and observation.
func (s *Service) CaptureConversation() (mo.Option[modelexecution.CapturedConversation], error) {
	if s.policyRuntime == nil {
		return mo.None[modelexecution.CapturedConversation](), errors.New("conversation policy runtime is unavailable")
	}
	owners := s.policyRuntime.SnapshotCompactionPolicies()
	if len(owners) == 0 {
		return mo.None[modelexecution.CapturedConversation](), nil
	}
	if len(owners) != 1 {
		return mo.None[modelexecution.CapturedConversation](), errors.New(
			"multiple compaction policy owners are registered",
		)
	}
	owner := owners[0]
	binding, err := s.IssueContext(owner.ExtensionID)
	if err != nil {
		return mo.None[modelexecution.CapturedConversation](), fmt.Errorf(
			"issue captured conversation policy context: %w",
			err,
		)
	}
	if binding.RuntimeInstanceID != owner.RuntimeID {
		return mo.None[modelexecution.CapturedConversation](), errors.New(
			"captured conversation policy generation is unavailable",
		)
	}
	s.mutex.Lock()
	incarnation := s.bindings[owner.ExtensionID].incarnation
	s.mutex.Unlock()
	captured := &capturedConversation{
		service:     s,
		owner:       owner,
		binding:     binding,
		incarnation: incarnation,
		handlers:    s.policyRuntime.SnapshotCompactionHandlers(),
	}
	return mo.Some[modelexecution.CapturedConversation](captured), nil
}

// validate checks the admitted runtime and session context.
func (c *capturedConversation) validate() error {
	return c.service.ValidateContext(
		c.owner.ExtensionID,
		c.owner.RuntimeID,
		extension.ContextRef{
			ID:                c.binding.ID,
			RuntimeInstanceID: c.binding.RuntimeInstanceID,
			SessionID:         c.binding.SessionID,
		},
	)
}

// newLifetime admits non-authorizing policy work.
func (s *Service) newLifetime(ctx context.Context, owner PolicyOwner) *compactionOperation {
	parent, cancel := context.WithCancel(ctx)
	operation := &compactionOperation{
		authorityID:          "",
		lifecycleID:          rand.Text(),
		owner:                owner,
		snapshot:             CompactionSnapshot{},
		requests:             nil,
		generators:           nil,
		results:              nil,
		context:              parent,
		cancel:               cancel,
		mutex:                sync.Mutex{},
		children:             sync.WaitGroup{},
		closed:               false,
		replacementRequested: false,
		receipt:              mo.None[session.Entry](),
		causes:               nil,
		terminalError:        nil,
		state:                mo.None[contextcompaction.PolicyState](),
	}
	s.mutex.Lock()
	s.compactionOperations[operation.lifecycleID] = operation
	s.mutex.Unlock()
	return operation
}

// finishLifetime settles nested work.
func (c *capturedConversation) finishLifetime(ctx context.Context, operation *compactionOperation, cause error) error {
	_, acquired, terminal := operation.finish()
	c.service.mutex.Lock()
	delete(c.service.compactionOperations, operation.lifecycleID)
	c.service.mutex.Unlock()
	cause = errors.Join(cause, terminal, context.Cause(ctx))
	if cause != nil {
		return errors.Join(cause, acquired)
	}
	if acquired != nil && !modelexecution.IsPureObservationCancellation(operation.context, acquired) {
		return acquired
	}
	return nil
}

// Observe reports the successful provider attempt to the admitted policy generation.
func (c *capturedConversation) Observe(
	ctx context.Context,
	input modelexecution.ProviderRequest,
	response model.Response,
) error {
	if err := c.validate(); err != nil {
		return err
	}
	operation := c.service.newLifetime(ctx, c.owner)
	err := c.service.policyRuntime.ObserveConversation(
		operation.context,
		c.owner,
		c.binding,
		ConversationObservation{Input: input, Response: response, LifecycleID: operation.lifecycleID},
	)
	err = c.finishLifetime(ctx, operation, err)
	if err != nil {
		return fmt.Errorf(
			"observe conversation with policy %q generation %q: %w",
			c.owner.ExtensionID,
			c.owner.RuntimeID,
			err,
		)
	}
	return nil
}
