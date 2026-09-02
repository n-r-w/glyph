//go:build integration

package logging

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenUICreatesOwnerOnlyAppendStructuredLog verifies the UI log sink contract.
// It tests the logging adapter itself, not application behavior inferred from emitted logs.
func TestOpenUICreatesOwnerOnlyAppendStructuredLog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "logs")
	path := filepath.Join(directory, "glyph.log")

	logger, file, err := OpenUI(directory, path)
	require.NoError(t, err)
	logger.InfoContext(context.WithoutCancel(t.Context()), "first record", "mode", "ui")
	require.NoError(t, file.Close())
	logger, file, err = OpenUI(directory, path)
	require.NoError(t, err)
	logger.ErrorContext(context.WithoutCancel(t.Context()), "second record", "safe", true)
	require.NoError(t, file.Close())

	directoryInfo, err := os.Stat(directory)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), directoryInfo.Mode().Perm())
	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		assert.NotEmpty(t, record["msg"])
	}
}
