//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// ProgrammaticAppSuite exercises the owning process through its generated client.
type ProgrammaticAppSuite struct {
	suite.Suite
}

// TestClientAppSuites runs client process contracts with exclusive provider transport access.
//
//nolint:paralleltest // Suite cases temporarily replace the process-wide HTTP transport.
func TestClientAppSuites(t *testing.T) {
	t.Run("Programmatic", func(t *testing.T) { suite.Run(t, new(ProgrammaticAppSuite)) })
	t.Run("UIRunFailure", func(t *testing.T) { suite.Run(t, new(UIRunFailureSuite)) })
}
