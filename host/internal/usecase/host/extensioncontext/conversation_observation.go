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

// CaptureConversationObservation retains one admitted owner and its ordinary session binding.
func (s *Service) CaptureConversationObservation() (mo.Option[modelexecution.ConversationObservation], error) {
	if s.policyRuntime == nil {
		return mo.None[modelexecution.ConversationObservation](), errors.New(
			"conversation policy runtime is unavailable",
		)
	}
	owners := s.policyRuntime.SnapshotCompactionPolicies()
	if len(owners) == 0 {
		return mo.None[modelexecution.ConversationObservation](), nil
	}
	if len(owners) != 1 {
		return mo.None[modelexecution.ConversationObservation](), errors.New(
			"multiple compaction policy owners are registered",
		)
	}
	owner := owners[0]
	binding, bindingErr := s.IssueContext(owner.ExtensionID)
	if bindingErr != nil {
		return mo.None[modelexecution.ConversationObservation](), fmt.Errorf(
			"issue captured conversation policy context: %w",
			bindingErr,
		)
	}
	if binding.RuntimeInstanceID != owner.RuntimeID {
		return mo.None[modelexecution.ConversationObservation](), errors.New(
			"captured conversation policy generation is unavailable",
		)
	}
	observe := modelexecution.ConversationObservation(
		func(ctx context.Context, input modelexecution.ProviderRequest, response model.Response) error {
			reference := extension.ContextRef{
				ID:                binding.ID,
				RuntimeInstanceID: binding.RuntimeInstanceID,
				SessionID:         binding.SessionID,
			}
			if err := s.ValidateContext(owner.ExtensionID, owner.RuntimeID, reference); err != nil {
				return err
			}
			lifetimeID := rand.Text()
			parent, cancel := context.WithCancel(ctx)
			operation := &compactionOperation{
				authorityID:          "",
				lifecycleID:          lifetimeID,
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
			s.compactionOperations[lifetimeID] = operation
			s.mutex.Unlock()
			err := s.policyRuntime.ObserveConversation(
				parent,
				owner,
				binding,
				ConversationObservation{Input: input, Response: response, LifecycleID: lifetimeID},
			)
			_, acquired, terminal := operation.finish()
			s.mutex.Lock()
			delete(s.compactionOperations, lifetimeID)
			s.mutex.Unlock()
			err = errors.Join(err, terminal, context.Cause(ctx))
			if err != nil {
				err = errors.Join(err, acquired)
			} else if acquired != nil && !modelexecution.IsPureObservationCancellation(parent, acquired) {
				err = acquired
			}
			if err != nil {
				return fmt.Errorf(
					"observe conversation with policy %q generation %q: %w",
					owner.ExtensionID,
					owner.RuntimeID,
					err,
				)
			}
			return nil
		},
	)
	return mo.Some(observe), nil
}
