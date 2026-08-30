package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// CommitNavigation atomically persists and publishes one no-summary active-leaf change.
func (s *Service) CommitNavigation(
	ctx context.Context,
	expectedActiveLeafID mo.Option[string],
	destinationID mo.Option[string],
) (session.Tree, error) {
	if err := ctx.Err(); err != nil {
		return session.Tree{}, err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return session.Tree{}, err
	}
	if s.writeUnavailable {
		return session.Tree{}, session.ErrPersistenceUnavailable
	}
	if s.active.Tree.ActiveLeafID() != expectedActiveLeafID {
		return session.Tree{}, errors.New("commit session navigation: active leaf changed")
	}

	candidateTree := cloneTree(s.active.Tree)
	if err := candidateTree.SetActiveLeaf(destinationID); err != nil {
		return session.Tree{}, fmt.Errorf("validate session navigation destination: %w", err)
	}
	result, err := s.repository.Apply(ctx, ApplyCommand{
		Header:      s.active.Header,
		StoragePath: s.active.StoragePath,
		Mutation: Mutation{
			Entry: mo.None[session.Entry](),
			Navigation: mo.Some(NavigationMutation{
				DestinationID: destinationID,
				BranchSummary: mo.None[session.Entry](),
			}),
			Label:              mo.None[LabelMutation](),
			SessionInformation: mo.None[SessionInformationMutation](),
		},
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationNavigation, s.active.Header.ID, err)
		// Keep the preceding durable snapshot readable while blocking later process-local mutations.
		s.writeUnavailable = true
		return session.Tree{}, fmt.Errorf("%w: commit session navigation: %w", session.ErrPersistenceUnavailable, err)
	}

	// Publish the destination only after the repository synchronizes the complete navigation mutation.
	s.active.StoragePath = result.StoragePath
	s.active.Tree = candidateTree
	return cloneTree(candidateTree), nil
}
