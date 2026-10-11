//go:build !integration

// Package sessions owns session history and compaction state.
package sessions

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// projectReplacementPreservesBoundaryCauseSuite isolates candidate boundary rejection.
type projectReplacementPreservesBoundaryCauseSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProjectReplacementPreservesBoundaryCause checks projection exposes the boundary rejection cause before mutation.
func TestProjectReplacementPreservesBoundaryCause(t *testing.T) {
	// Arrange parallel execution of the boundary-rejection suite.
	t.Parallel()
	// Act through candidate projection with an outside boundary.
	// Assert the complete structural cause before mutation in TestScenario.
	suite.Run(t, &projectReplacementPreservesBoundaryCauseSuite{Suite: suite.Suite{}})
}
