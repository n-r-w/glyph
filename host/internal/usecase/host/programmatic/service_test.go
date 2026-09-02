//go:build !integration

package programmatic

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type ServiceSuite struct {
	suite.Suite
}

type selectionError struct {
	code SelectionCode
}

// Error returns the selection failure message used by service scenarios.
func (e selectionError) Error() string {
	return "safe selection failure: " + string(e.code)
}

// SelectionCode returns the typed selection failure code.
func (e selectionError) SelectionCode() string {
	return string(e.code)
}

// TestServiceSuite runs Programmatic service behavior scenarios.
func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// idleStateSnapshot returns a stable idle run-state fixture.
func idleStateSnapshot() run.State {
	return run.State{}
}

// emptyHistorySnapshot returns an empty public history fixture.
func emptyHistorySnapshot() []agent.HistoryEntry {
	return nil
}

// cancelActiveTestRun cancels and joins the active test run when present.
func cancelActiveTestRun(service *Service) error {
	active := service.delivery.activeSnapshot()
	if active == nil {
		return nil
	}
	return service.delivery.cancelAndWait(active)
}
