// Package headless parses one-shot commands and renders Host events without UI dependencies.
package headless

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// Service executes one parsed headless request.
type Service struct {
	// runner executes the validated user request.
	runner AgentRunner
}

// New creates a headless request controller.
func New(runner AgentRunner) *Service {
	return &Service{runner: runner}
}

// Execute runs the request and accepts only a completed terminal outcome.
func (s *Service) Execute(ctx context.Context, userText string) error {
	outcome, err := s.runner.Run(ctx, userText)
	if err != nil {
		return fmt.Errorf("run headless agent: %w", err)
	}
	if outcome != agent.RunOutcomeCompleted {
		return fmt.Errorf("headless agent ended with outcome %d", outcome)
	}
	return nil
}
