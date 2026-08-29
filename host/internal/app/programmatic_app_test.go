//go:build integration

package app

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/samber/mo"

	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
)

// ProgrammaticAppSuite exercises the owning process through its generated client.
type ProgrammaticAppSuite struct {
	suite.Suite
}

// TestProgrammaticAppSuite runs the real Unix-socket process contract.
//
//nolint:paralleltest // Suite cases temporarily replace the process-wide HTTP transport.
func TestProgrammaticAppSuite(t *testing.T) {
	suite.Run(t, new(ProgrammaticAppSuite))
}

// newIdleProgrammaticTestSession creates an idle concrete session for application arbitration tests.
func newIdleProgrammaticTestSession(t *testing.T) *hostprogrammatic.Service {
	t.Helper()
	return hostprogrammatic.New(
		hostprogrammatic.NewMockCoordinator(gomock.NewController(t)), nil,
		func() agentrun.State {
			return agentrun.State{
				Status: agentrun.StatusIdle, RunID: mo.None[string](),
				PartialResponse: mo.None[model.Response](), ToolPreviews: nil,
			}
		},
		func() []agent.HistoryEntry { return nil }, nil, hostprogrammatic.NewDelivery(),
	)
}
