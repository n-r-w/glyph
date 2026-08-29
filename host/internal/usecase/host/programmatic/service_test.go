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

func (e selectionError) Error() string {
	return "safe selection failure: " + string(e.code)
}

func (e selectionError) SelectionCode() string {
	return string(e.code)
}

func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

func idleStateSnapshot() run.State {
	return run.State{}
}

func emptyHistorySnapshot() []agent.HistoryEntry {
	return nil
}
