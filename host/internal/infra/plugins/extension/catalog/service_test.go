//go:build integration

package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
)

// TestServiceDiscoverObservesAllExecutables retains duplicate and empty identities for Host acceptance.
func TestServiceDiscoverObservesAllExecutables(t *testing.T) {
	t.Parallel()
	// Arrange executable candidates and one non-executable file.
	directory := t.TempDir()
	for _, name := range []string{"Good_Tool", "good tool", "Other__Tool", "___"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("fixture"), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "not-executable"), []byte("fixture"), 0o600))
	// Act through the real filesystem adapter.
	discovery, err := New().Discover(t.Context(), extensionruntime.Directory{Path: directory})
	// Assert filesystem name order and all normalized observations.
	require.NoError(t, err)
	require.NoError(t, discovery.DirectoryError)
	assert.Equal(t, []extensionruntime.Executable{
		{ID: "good-tool", Path: filepath.Join(directory, "Good_Tool")},
		{ID: "other-tool", Path: filepath.Join(directory, "Other__Tool")},
		{ID: "", Path: filepath.Join(directory, "___")},
		{ID: "good-tool", Path: filepath.Join(directory, "good tool")},
	}, discovery.Candidates)
	assert.Empty(t, discovery.Issues)
}

// TestServiceDiscoverDirectoryFailure retains the filesystem cause for Host acceptance.
func TestServiceDiscoverDirectoryFailure(t *testing.T) {
	t.Parallel()
	// Arrange a missing directory.
	missing := filepath.Join(t.TempDir(), "missing")
	// Act through directory inspection.
	result, err := New().Discover(t.Context(), extensionruntime.Directory{Path: missing})
	// Assert that absence remains an observation with its complete filesystem cause.
	require.NoError(t, err)
	require.ErrorIs(t, result.DirectoryError, os.ErrNotExist)
	require.ErrorContains(t, result.DirectoryError, missing)
	assert.Empty(t, result.Candidates)
	assert.Empty(t, result.Issues)
}
