package extensionruntime

import (
	"context"
	"errors"
	"log/slog"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
)

// StopReporting stops reporter admission and joins callbacks without changing process lifetime.
func (s *Service) StopReporting() {
	s.mutex.Lock()
	s.reportingStopped = true
	s.mutex.Unlock()
	s.reporting.Wait()
}

// report transfers an admitted source to output or retains an unreported failure for runtime cleanup.
func (s *Service) report(ctx context.Context, failure extension.RuntimeFailure) {
	s.mutex.Lock()
	if s.reportingStopped {
		// Work and monitor joins keep this source registration ahead of Close's final collection.
		message, err := failure.Message()
		if err == nil {
			err = errors.New(message)
		}
		s.reportErrors = append(s.reportErrors, err)
		s.mutex.Unlock()
		return
	}
	// Registration and the reporting barrier use the same lock, so no Add can follow Wait.
	s.reporting.Add(1)
	s.mutex.Unlock()
	defer s.reporting.Done()

	if err := s.reporter.ReportRuntimeFailure(ctx, failure); err != nil {
		// Output owns its transferred source. Keep only the reporting result here.
		s.mutex.Lock()
		s.reportErrors = append(s.reportErrors, err)
		s.mutex.Unlock()
		slog.ErrorContext(ctx, "report extension runtime failure",
			"plugin_id", failure.PluginID, "condition", failure.Condition, "error", err)
	}
}
