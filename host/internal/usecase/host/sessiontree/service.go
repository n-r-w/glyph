package sessiontree

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
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
func (s *Service) NavigateTree(ctx context.Context, targetID string) (sessionnavigation.Result, error) {
	if err := ctx.Err(); err != nil {
		return sessionnavigation.Result{}, err
	}
	tree := s.active.Tree()
	expectedActiveLeafID := tree.ActiveLeafID()
	preparation, err := tree.NavigationPreparation(targetID)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	if cancellationErr := ctx.Err(); cancellationErr != nil {
		return sessionnavigation.Result{}, cancellationErr
	}
	committed, err := s.active.CommitNavigation(ctx, expectedActiveLeafID, preparation.DestinationID)
	if err != nil {
		return sessionnavigation.Result{}, err
	}
	return sessionnavigation.Result{
		Tree:         committed,
		ActiveLeafID: committed.ActiveLeafID(),
		ActiveBranch: committed.ActiveBranch(),
		NextInput:    preparation.NextInput,
	}, nil
}
