// Package ui receives UI commands and owns their transport operation lifecycles.
package ui

import (
	"context"
	"fmt"
)

// Service receives commands from the selected process stream.
type Service struct {
	// source retains the selected process and opens its single stream.
	source StreamSource
	// connection supplies the stream acquired at the startup boundary.
	connection Connection
}

// New creates a controller before session and provider assembly.
func New(source StreamSource) *Service {
	return &Service{source: source, connection: nil}
}

// Open acquires the selected stream without restarting the process.
func (s *Service) Open(ctx context.Context) error {
	connection, err := s.source.Open(ctx)
	if err != nil {
		return err
	}
	s.connection = connection
	return nil
}

// Execute initializes Host output, then receives commands while asynchronous readiness work runs.
func (s *Service) Execute(ctx context.Context, session Session) error {
	if err := session.Initialize(ctx); err != nil {
		return fmt.Errorf("execute UI session: %w", err)
	}
	if err := s.runOperations(ctx, session); err != nil {
		return fmt.Errorf("execute UI session: run UI operations: %w", err)
	}
	return nil
}
