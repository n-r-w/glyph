// Package ui starts one Host UI session after application composition.
package ui

import (
	"context"
	"fmt"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// Service delegates selected UI control to the session use case.
type Service struct {
	session Session
}

// New creates a UI controller.
func New(session Session) *Service {
	return &Service{session: session}
}

// Execute runs the selected UI lifecycle until quit or stream termination.
func (s *Service) Execute(ctx context.Context, initialization domainui.Initialization) error {
	if err := s.session.Run(ctx, initialization); err != nil {
		return fmt.Errorf("execute UI session: %w", err)
	}
	return nil
}
