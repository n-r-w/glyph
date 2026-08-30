//go:build integration

package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
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
	discovery, err := New().Discover(t.Context(), domainui.Directory{Path: directory})

	// Assert: only executable regular files remain in normalized ID order.
	require.NoError(t, err)
	assert.Equal(t, []domainui.Candidate{
		{ID: "first-ui", Path: filepath.Join(directory, " first  UI ")},
		{ID: "second-ui", Path: filepath.Join(directory, "Second_UI")},
	}, discovery.Candidates)
}

// TestDiscoverRejectsDirectoryFailure verifies missing and unreadable effective directories fail.
func TestDiscoverRejectsDirectoryFailure(t *testing.T) {
	t.Parallel()

	_, err := New().Discover(t.Context(), domainui.Directory{Path: filepath.Join(t.TempDir(), "missing")})

	require.Error(t, err)
	assert.ErrorContains(t, err, "read UI directory")
}

// TestDiscoverRejectsEmptyNormalizedID verifies one invalid executable invalidates the full catalog.
func TestDiscoverRejectsEmptyNormalizedID(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeCandidate(t, directory, "___---", 0o755)
	writeCandidate(t, directory, "valid", 0o755)

	discovery, err := New().Discover(t.Context(), domainui.Directory{Path: directory})

	require.Error(t, err)
	assert.Empty(t, discovery.Candidates)
	assert.ErrorContains(t, err, "empty normalized ID")
}

// TestDiscoverRejectsDuplicateNormalizedIDs verifies any duplicate group invalidates the full catalog.
func TestDiscoverRejectsDuplicateNormalizedIDs(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeCandidate(t, directory, "Duplicate_UI", 0o755)
	writeCandidate(t, directory, "duplicate ui", 0o755)
	writeCandidate(t, directory, "valid", 0o755)

	discovery, err := New().Discover(t.Context(), domainui.Directory{Path: directory})

	require.Error(t, err)
	assert.Empty(t, discovery.Candidates)
	assert.ErrorContains(t, err, "duplicate normalized ID")
}

// writeCandidate creates one deterministic catalog entry.
func writeCandidate(t *testing.T, directory, name string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("plugin"), mode))
}
