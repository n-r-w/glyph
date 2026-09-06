//go:build integration

package catalog

import (
	"os"
	"path/filepath"
	"testing"

	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscoverReturnsSortedExecutableCandidates verifies filtering and shared ID normalization.
func TestDiscoverReturnsSortedExecutableCandidates(t *testing.T) {
	t.Parallel()

	// Arrange: create executable and ignored entries in one effective directory.
	directory := t.TempDir()
	writeCandidate(t, directory, "Second_UI", 0o755)
	writeCandidate(t, directory, " first  UI ", 0o700)
	writeCandidate(t, directory, "ignored", 0o600)
	require.NoError(t, os.Mkdir(filepath.Join(directory, "directory"), 0o755))

	// Act: discover the complete catalog.
	discovery, err := New().Discover(t.Context(), hostui.Directory{Path: directory})

	// Assert: only executable regular files remain in normalized ID order.
	require.NoError(t, err)
	assert.Equal(t, []hostui.Candidate{
		{ID: "first-ui", Path: filepath.Join(directory, " first  UI ")},
		{ID: "second-ui", Path: filepath.Join(directory, "Second_UI")},
	}, discovery.Candidates)
}

// TestDiscoverRecordsDirectoryFailure retains the complete directory cause for UI acceptance.
func TestDiscoverRecordsDirectoryFailure(t *testing.T) {
	t.Parallel()
	// Arrange a missing effective directory.
	path := filepath.Join(t.TempDir(), "missing")
	// Act through the real filesystem adapter.
	discovery, err := New().Discover(t.Context(), hostui.Directory{Path: path})
	// Assert that the read failure remains distinct from discovery cancellation.
	require.NoError(t, err)
	require.ErrorIs(t, discovery.DirectoryError, os.ErrNotExist)
	require.ErrorContains(t, discovery.DirectoryError, path)
}

// TestDiscoverRecordsEmptyNormalizedID retains the executable for UI acceptance.
func TestDiscoverRecordsEmptyNormalizedID(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeCandidate(t, directory, "___---", 0o755)
	writeCandidate(t, directory, "valid", 0o755)

	discovery, err := New().Discover(t.Context(), hostui.Directory{Path: directory})

	require.NoError(t, err)
	assert.Equal(t, []hostui.Candidate{
		{ID: "", Path: filepath.Join(directory, "___---")},
		{ID: "valid", Path: filepath.Join(directory, "valid")},
	}, discovery.Candidates)
}

// TestDiscoverRecordsDuplicateNormalizedIDs retains each executable for UI acceptance.
func TestDiscoverRecordsDuplicateNormalizedIDs(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeCandidate(t, directory, "Duplicate_UI", 0o755)
	writeCandidate(t, directory, "duplicate ui", 0o755)
	writeCandidate(t, directory, "valid", 0o755)

	discovery, err := New().Discover(t.Context(), hostui.Directory{Path: directory})

	require.NoError(t, err)
	assert.Equal(t, []hostui.Candidate{
		{ID: "duplicate-ui", Path: filepath.Join(directory, "Duplicate_UI")},
		{ID: "duplicate-ui", Path: filepath.Join(directory, "duplicate ui")},
		{ID: "valid", Path: filepath.Join(directory, "valid")},
	}, discovery.Candidates)
}

// writeCandidate creates one deterministic catalog entry.
func writeCandidate(t *testing.T, directory, name string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("plugin"), mode))
}
