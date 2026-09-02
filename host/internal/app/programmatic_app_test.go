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

// TestProgrammaticAppSuite runs the real Unix-socket process contract.
//
//nolint:paralleltest // Suite cases temporarily replace the process-wide HTTP transport.
func TestProgrammaticAppSuite(t *testing.T) {
	suite.Run(t, new(ProgrammaticAppSuite))
}
