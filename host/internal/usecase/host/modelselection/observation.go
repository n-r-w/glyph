package modelselection

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

const (
	// IssueCodeObserverError identifies a post-commit selection observer failure.
	IssueCodeObserverError = "OBSERVER_ERROR"
	// IssueCodeDeliveryFailed identifies a failed post-commit client delivery.
	IssueCodeDeliveryFailed = "DELIVERY_FAILED"
)

// SelectionChange contains detached values from one atomic selection commit.
type SelectionChange struct {
	// Preceding is the complete selection before the commit.
	Preceding model.Selection
	// Committed is the complete committed selection.
	Committed model.Selection
}

// observeSelection delivers the committed change through the bound selection observer.
func (s *Service) observeSelection(ctx context.Context, change SelectionChange, issues []Issue) ([]Issue, error) {
	s.mutex.Lock()
	observer := s.observer
	s.mutex.Unlock()
	if observer == nil {
		return issues, nil
	}
	observed, err := observer.ObserveSelection(ctx, change)
	return append(issues, observed...), err
}
