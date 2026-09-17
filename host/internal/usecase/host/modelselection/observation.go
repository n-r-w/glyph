package modelselection

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// ObservationKind identifies one changed part of a committed selection.
type ObservationKind uint8

const (
	// ObservationKindReasoning identifies a committed reasoning-choice change.
	ObservationKindReasoning ObservationKind = iota + 1
	// ObservationKindModel identifies a committed provider or model change.
	ObservationKindModel
	// IssueCodeObserverError identifies an ordinary post-commit observer failure.
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

// observeSelection invokes changed observer groups in their required order.
func (s *Service) observeSelection(ctx context.Context, change SelectionChange, issues []Issue) []Issue {
	s.mutex.Lock()
	observer := s.observer
	s.mutex.Unlock()
	if observer == nil {
		return issues
	}
	if change.Preceding.ReasoningChoice != change.Committed.ReasoningChoice {
		issues = append(issues, observer.ObserveSelection(ctx, ObservationKindReasoning, change)...)
	}
	if change.Preceding.Provider != change.Committed.Provider || change.Preceding.Model != change.Committed.Model {
		issues = append(issues, observer.ObserveSelection(ctx, ObservationKindModel, change)...)
	}
	return issues
}
