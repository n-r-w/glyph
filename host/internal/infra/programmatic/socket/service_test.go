package socket

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCreatesUniqueAutomaticSocketPaths(t *testing.T) {
	t.Parallel()

	first, err := New(t.Context(), "")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, first.Close()) })
	second, err := New(t.Context(), "")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, second.Close()) })

	assert.NotEqual(t, first.Path(), second.Path())
	assert.True(t, filepath.IsAbs(first.Path()))
	assert.Equal(t, "control.sock", filepath.Base(first.Path()))
}

func TestNewSecuresAutomaticDirectoryAndSocket(t *testing.T) {
	t.Parallel()

	service, err := New(t.Context(), "")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, service.Close()) })

	directoryInfo, err := os.Stat(filepath.Dir(service.Path()))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), directoryInfo.Mode().Perm())
	socketInfo, err := os.Stat(service.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), socketInfo.Mode().Perm())
}

func TestNewResolvesExplicitPathToAbsolute(t *testing.T) {
	t.Parallel()

	targetPath := filepath.Join(shortTempDir(t), "control.sock")
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	relativePath, err := filepath.Rel(workingDirectory, targetPath)
	require.NoError(t, err)
	expectedPath, err := filepath.Abs(relativePath)
	require.NoError(t, err)

	service, err := New(t.Context(), relativePath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, service.Close()) })

	assert.Equal(t, expectedPath, service.Path())
}

func TestNewRejectsMissingExplicitParent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "control.sock")

	service, err := New(t.Context(), path)

	assert.Nil(t, service)
	require.Error(t, err)
	require.ErrorContains(t, err, "socket parent")
}

func TestNewRejectsExistingExplicitPath(t *testing.T) {
	t.Parallel()

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "control.sock")
		require.NoError(t, os.WriteFile(path, []byte("owned"), 0o600))

		service, err := New(t.Context(), path)

		assert.Nil(t, service)
		require.Error(t, err)
		require.ErrorContains(t, err, "already exists")
		contents, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("owned"), contents)
	})

	t.Run("dangling symbolic link", func(t *testing.T) {
		t.Parallel()

		parent := t.TempDir()
		path := filepath.Join(parent, "control.sock")
		require.NoError(t, os.Symlink(filepath.Join(parent, "missing"), path))

		service, err := New(t.Context(), path)

		assert.Nil(t, service)
		require.Error(t, err)
		require.ErrorContains(t, err, "already exists")
	})
}

func TestCloseClosesListenerAndRemovesAutomaticPaths(t *testing.T) {
	t.Parallel()

	service, err := New(t.Context(), "")
	require.NoError(t, err)
	path := service.Path()
	directory := filepath.Dir(path)

	require.NoError(t, service.Close())

	_, err = service.Accept()
	require.Error(t, err)
	require.ErrorIs(t, err, net.ErrClosed)
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestCloseRemovesPathsAfterServerClosesListener verifies idempotent listener cleanup.
func TestCloseRemovesPathsAfterServerClosesListener(t *testing.T) {
	t.Parallel()

	service, err := New(t.Context(), "")
	require.NoError(t, err)
	path := service.Path()
	directory := filepath.Dir(path)
	require.NoError(t, service.Listener.Close())

	require.NoError(t, service.Close())
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCloseRemovesExplicitSocketAndRetainsCallerParent(t *testing.T) {
	t.Parallel()

	parent := shortTempDir(t)
	require.NoError(t, os.Chmod(parent, 0o750))
	path := filepath.Join(parent, "control.sock")
	service, err := New(t.Context(), path)
	require.NoError(t, err)

	socketInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), socketInfo.Mode().Perm())
	require.NoError(t, service.Close())

	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	parentInfo, err := os.Stat(parent)
	require.NoError(t, err)
	assert.True(t, parentInfo.IsDir())
	assert.Equal(t, os.FileMode(0o750), parentInfo.Mode().Perm())
}

func shortTempDir(t *testing.T) string {
	t.Helper()

	directory, err := os.MkdirTemp("", "glyph-socket-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(directory)) })
	return directory
}
