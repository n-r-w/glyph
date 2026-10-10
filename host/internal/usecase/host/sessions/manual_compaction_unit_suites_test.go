//go:build !integration

package sessions

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// projectReplacementPreservesBoundaryCauseSuite owns an independent scenario context.
type projectReplacementPreservesBoundaryCauseSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProjectReplacementPreservesBoundaryCause checks projection exposes the boundary rejection cause before mutation.
func TestProjectReplacementPreservesBoundaryCause(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &projectReplacementPreservesBoundaryCauseSuite{Suite: suite.Suite{}})
}
