package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitializeAtCreatesAndEnforcesOwnerOnlyDirectory verifies stable paths and corrected permissions.
func TestInitializeAtCreatesAndEnforcesOwnerOnlyDirectory(t *testing.T) {
	t.Parallel()

	homeDirectory := t.TempDir()
	glyphDirectory := filepath.Join(homeDirectory, ".glyph")
	require.NoError(t, os.Mkdir(glyphDirectory, 0o755))

	paths, err := initializeAt(homeDirectory)

	require.NoError(t, err)
	assert.Equal(t, glyphDirectory, paths.Directory)
	assert.Equal(t, filepath.Join(glyphDirectory, "settings.yaml"), paths.SettingsFile)
	assert.Equal(t, filepath.Join(glyphDirectory, "credentials.json"), paths.CredentialsFile)
	info, err := os.Stat(glyphDirectory)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// TestInitializeAtCreatesMissingDirectory verifies first-run initialization.
func TestInitializeAtCreatesMissingDirectory(t *testing.T) {
	t.Parallel()

	homeDirectory := t.TempDir()

	paths, err := initializeAt(homeDirectory)

	require.NoError(t, err)
	info, err := os.Stat(paths.Directory)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}
