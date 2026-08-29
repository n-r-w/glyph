//go:build integration

package app

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildToolsExecutable compiles the real tools command into a test-owned temporary directory.
func buildToolsExecutable(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	output := filepath.Join(t.TempDir(), "glyph-tools")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", output, "./plugins/extension/tools/cmd/glyph-tools")
	command.Dir = root
	outputBytes, err := command.CombinedOutput()
	require.NoError(t, err, string(outputBytes))
	return filepath.Dir(output)
}
