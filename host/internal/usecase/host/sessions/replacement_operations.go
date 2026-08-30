package sessions

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// ForkActive creates a replacement session before one selected user message.
func (s *Service) ForkActive(ctx context.Context, targetID string) (session.Replacement, string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	preparation, err := s.active.Tree.NavigationPreparation(targetID)
	if err != nil {
		return session.Replacement{}, "", err
	}
	nextInput, isUser := preparation.NextInput.Get()
	if !isUser {
		return session.Replacement{}, "", session.ErrInvalidForkTarget
	}
	entries, err := s.active.Tree.BranchTo(preparation.DestinationID)
	if err != nil {
		return session.Replacement{}, "", err
	}
	retained, err := retainedTree(entries, preparation.DestinationID, s.active.Tree.Labels())
	if err != nil {
		return session.Replacement{}, "", fmt.Errorf("build forked session tree: %w", err)
	}
	replacement, err := s.createReplacement(ctx, retained)
	if err != nil {
		return session.Replacement{}, "", err
	}
	return replacement, nextInput, nil
}

// CloneActive creates a replacement session from the complete active branch.
func (s *Service) CloneActive(ctx context.Context) (session.Replacement, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	activeLeafID := s.active.Tree.ActiveLeafID()
	retained, err := retainedTree(s.active.Tree.ActiveBranch(), activeLeafID, s.active.Tree.Labels())
	if err != nil {
		return session.Replacement{}, fmt.Errorf("build cloned session tree: %w", err)
	}
	return s.createReplacement(ctx, retained)
}

// SetLabel persists one active-session entry label mutation.
func (s *Service) SetLabel(ctx context.Context, targetID, label string) (session.Tree, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	candidate := s.active.Tree.Clone()
	if err := candidate.SetLabel(targetID, label); err != nil {
		return session.Tree{}, session.ErrEntryNotFound
	}
	if s.writeUnavailable {
		return session.Tree{}, session.ErrPersistenceUnavailable
	}
	result, err := s.repository.Apply(ctx, ApplyCommand{
		Header: s.active.Header, StoragePath: s.active.StoragePath,
		Mutation: Mutation{
			Entry: mo.None[session.Entry](), Navigation: mo.None[NavigationMutation](),
			Label:              mo.Some(LabelMutation{TargetID: targetID, Label: label}),
			SessionInformation: mo.None[SessionInformationMutation](),
		},
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationLabel, s.active.Header.ID, err)
		s.writeUnavailable = true
		return session.Tree{}, fmt.Errorf("%w: set session entry label: %w", session.ErrPersistenceUnavailable, err)
	}
	s.active.StoragePath = result.StoragePath
	s.active.Tree = candidate
	return candidate.Clone(), nil
}

// createReplacement persists and publishes one prepared replacement while the caller holds the service mutex.
func (s *Service) createReplacement(ctx context.Context, tree session.Tree) (session.Replacement, error) {
	id, err := s.ids.NewID()
	if err != nil {
		return session.Replacement{}, fmt.Errorf("create replacement session ID: %w", err)
	}
	header := session.Header{
		Version: formatVersion, ID: session.ID(id), CreatedAt: s.clock.Now(), WorkingDirectory: s.workingDirectory,
	}
	result, err := s.repository.CreateSnapshot(ctx, CreateSnapshotCommand{
		Header: header, Tree: tree, Information: s.active.Information,
		InformationUpdatedAt: s.active.InformationUpdatedAt,
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationReplacement, s.active.Header.ID, err)
		return session.Replacement{}, fmt.Errorf(
			"%w: create replacement session: %w",
			session.ErrPersistenceUnavailable,
			err,
		)
	}
	loaded := LoadedSession{
		Header: header, StoragePath: result.StoragePath, Tree: tree,
		Information: s.active.Information, InformationUpdatedAt: s.active.InformationUpdatedAt,
	}
	s.active = loaded
	s.history = sessiontree.HistoryFromEntries(tree.ActiveBranch())
	s.writeUnavailable = false
	return loaded.Replacement(), nil
}

// retainedTree copies one branch and keeps labels only for entries in that branch.
func retainedTree(
	entries []session.Entry,
	activeLeafID mo.Option[string],
	labels map[string]string,
) (session.Tree, error) {
	retainedIDs := lo.Map(entries, func(entry session.Entry, _ int) string {
		return entry.ID
	})
	return session.NewTree(entries, activeLeafID, lo.PickByKeys(labels, retainedIDs))
}
