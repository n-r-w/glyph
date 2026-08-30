package sessiontree

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
)

// Service coordinates navigation preparation and its atomic active-session commit.
type Service struct {
	// active supplies the immutable preparation snapshot and owns the commit.
	active ActiveSession
}

var _ sessioncontrol.Navigator = (*Service)(nil)

// New creates an internal session-tree navigation service.
func New(active ActiveSession) *Service {
	return &Service{active: active}
}

// NavigateTree moves the active leaf to the destination selected by the target entry.
func (s *Service) NavigateTree(ctx context.Context, targetID string) (sessioncontrol.NavigationResult, error) {
	if err := ctx.Err(); err != nil {
		return sessioncontrol.NavigationResult{}, err
	}
	tree := s.active.Tree()
	expectedActiveLeafID := tree.ActiveLeafID()
	preparation, err := tree.NavigationPreparation(targetID)
	if err != nil {
		return sessioncontrol.NavigationResult{}, err
	}
	if cancellationErr := ctx.Err(); cancellationErr != nil {
		return sessioncontrol.NavigationResult{}, cancellationErr
	}
	committed, err := s.active.CommitNavigation(ctx, expectedActiveLeafID, preparation.DestinationID)
	if err != nil {
		return sessioncontrol.NavigationResult{}, err
	}
	return sessioncontrol.NavigationResult{
		ActiveLeafID: committed.ActiveLeafID(),
		NextInput:    preparation.NextInput,
	}, nil
}
