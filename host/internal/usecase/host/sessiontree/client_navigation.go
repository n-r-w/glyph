package sessiontree

import (
	"context"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// NavigateUI composes handlers for UI intent and projects the committed result without rereading session state.
func (s *Service) NavigateUI(
	ctx context.Context,
	intent ui.NavigationIntent,
	publish func(session.Tree) error,
	retryProgress func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error,
) (ui.NavigationCompletion, error) {
	result, err := s.navigate(ctx, NavigationRequest{
		TargetEntryID: intent.TargetEntryID,
		SummaryMode:   SummaryMode(intent.SummaryMode),
		CustomFocus:   intent.CustomFocus,
	}, publish, retryProgress)
	return result.uiCompletion(), err
}

// NavigateProgrammatic composes handlers and projects Programmatic completion from the committed result.
func (s *Service) NavigateProgrammatic(
	ctx context.Context,
	intent programmatic.NavigationIntent,
	publish func(session.Tree) error,
	retryProgress func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error,
) (programmatic.NavigationCompletion, error) {
	result, err := s.navigate(ctx, NavigationRequest{
		TargetEntryID: intent.TargetEntryID,
		SummaryMode:   SummaryMode(intent.SummaryMode),
		CustomFocus:   intent.CustomFocus,
	}, publish, retryProgress)
	return result.programmaticCompletion(), err
}
