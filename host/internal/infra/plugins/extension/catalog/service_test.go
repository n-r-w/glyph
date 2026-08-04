package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// TestServiceDiscoverNormalizesAndIsolatesInvalidIDs keeps unaffected executable candidates.
func TestServiceDiscoverNormalizesAndIsolatesInvalidIDs(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	for _, name := range []string{"Good_Tool", "good tool", "Other__Tool", "___"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("fixture"), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "not-executable"), []byte("fixture"), 0o600))

	discovery, err := New().Discover(t.Context(), toolservice.Directory{Path: directory, Explicit: true})

	require.NoError(t, err)
	assert.Equal(t, []toolservice.Candidate{{ID: "other-tool", Path: filepath.Join(directory, "Other__Tool")}}, discovery.Candidates)
	require.Len(t, discovery.Issues, 3)
}

// TestServiceDiscoverDirectoryPolicy distinguishes default warnings from explicit failures.
func TestServiceDiscoverDirectoryPolicy(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")
	defaultResult, err := New().Discover(t.Context(), toolservice.Directory{Path: missing, Explicit: false})
	require.NoError(t, err)
	assert.Empty(t, defaultResult.Candidates)
	assert.Empty(t, defaultResult.Issues)

	_, err = New().Discover(t.Context(), toolservice.Directory{Path: missing, Explicit: true})
	require.Error(t, err)
}
