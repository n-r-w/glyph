package project

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceRead verifies that the filesystem adapter preserves complete project-file text.
func TestServiceRead(t *testing.T) {
	t.Parallel()

	// Arrange: create a text file with content whose final newline must be preserved.
	projectDirectory := t.TempDir()
	filePath := projectDirectory + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, "first\nsecond\n"))
	service := New()

	// Act: read the file through the filesystem adapter.
	content, err := service.ReadFile(t.Context(), filePath)

	// Assert: return every byte as text without trimming or normalization.
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", content)
}

// TestServiceReadMissingFile verifies that a missing project file returns an explicit error.
func TestServiceReadMissingFile(t *testing.T) {
	t.Parallel()

	// Arrange: select a path that does not exist in an isolated temporary directory.
	filePath := t.TempDir() + "/missing.txt"
	service := New()

	// Act: request the missing file.
	content, err := service.ReadFile(t.Context(), filePath)

	// Assert: return no content and an actionable filesystem error.
	assert.Empty(t, content)
	require.Error(t, err)
	assert.ErrorContains(t, err, "read project file")
}

// TestServiceWriteFile replaces content directly without creating project-directory artifacts.
func TestServiceWriteFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	filePath := directory + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, "before"))
	service := New()

	err := service.WriteFile(t.Context(), filePath, "after")

	require.NoError(t, err)
	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Equal(t, "after", string(content))
	entries, readDirErr := os.ReadDir(directory)
	require.NoError(t, readDirErr)
	require.Len(t, entries, 1)
	assert.Equal(t, "notes.txt", entries[0].Name())
}

// TestServiceReadCanceled verifies that cancellation is observed before filesystem access begins.
func TestServiceReadCanceled(t *testing.T) {
	t.Parallel()

	// Arrange: cancel the operation before the adapter attempts to read the file.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service := New()

	// Act: issue a read with the canceled context.
	content, err := service.ReadFile(ctx, t.TempDir()+"/notes.txt")

	// Assert: cancellation remains identifiable across the filesystem boundary.
	assert.Empty(t, content)
	require.ErrorIs(t, err, context.Canceled)
}

// writeTestFile creates a test fixture while keeping file creation outside production code.
func writeTestFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
