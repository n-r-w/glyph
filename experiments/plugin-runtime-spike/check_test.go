//go:build integration

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const automatedCheckTimeout = 30 * time.Second

// runtimeSuite owns the executable used by process-level protocol checks.
type runtimeSuite struct {
	suite.Suite
	executable string
}

// SetupSuite builds the spike once so each check launches the same artifact.
func (s *runtimeSuite) SetupSuite() {
	// The built artifact must stay in the test temporary directory because child processes execute it.
	s.executable = filepath.Join(s.T().TempDir(), "plugin-runtime-spike")
	build := exec.CommandContext(s.T().Context(), "go", "build", "-o", s.executable, ".")
	output, err := build.CombinedOutput()
	require.NoError(s.T(), err, "build spike executable: %s", output)
}

// TestAutomatedRuntimeChecks proves the selected process contracts against real child processes.
// Input is the suite executable. Expected output marks every protocol, lifecycle, and isolation
// behavior as observed. Edge cases cover cancellation, process crash, tool-name collision, a
// handshake-only UI probe, and UI stream completion. The check has no dependency on other tests.
func (s *runtimeSuite) TestAutomatedRuntimeChecks() {
	ctx, cancel := context.WithTimeout(s.T().Context(), automatedCheckTimeout)
	s.T().Cleanup(cancel)

	report, err := runAutomatedChecks(ctx, s.executable)
	require.NoError(s.T(), err)
	assert.True(s.T(), report.protocolVersion)
	assert.True(s.T(), report.multipleExtensions)
	assert.True(s.T(), report.streaming)
	assert.True(s.T(), report.cancellation)
	assert.True(s.T(), report.crashIsolation)
	assert.True(s.T(), report.collisionCleanup)
	assert.True(s.T(), report.probeWithoutUI)
	assert.True(s.T(), report.bidirectionalUI)
	assert.True(s.T(), report.uiStreamCompletion)
}

// TestRuntimeSuite runs the process-level spike verification suite.
func TestRuntimeSuite(t *testing.T) {
	suite.Run(t, new(runtimeSuite))
}
