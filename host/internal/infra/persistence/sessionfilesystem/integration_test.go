package sessionfilesystem_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// TestAppendCreatesVersionedSynchronizedSessionFile verifies the first named entry creates one private JSONL file.
func TestAppendCreatesVersionedSynchronizedSessionFile(t *testing.T) {
	t.Parallel()

	// Arrange an initialized filesystem repository and a named session entry.
	root := filepath.Join(t.TempDir(), "sessions")
	canonical, err := sessionstore.CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := sessionstore.New(root, canonical, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	// Act by appending the first durable record.
	result, err := repository.Append(t.Context(), hostsessions.AppendCommand{
		Header:      session.Header{Version: 1, ID: "session-id", CreatedAt: createdAt, WorkingDirectory: canonical},
		StoragePath: "",
		Entry: session.Entry{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](), ID: "entry-id", CreatedAt: updatedAt, Information: mo.Some(session.Information{Name: "release notes"}), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost]()},
	})
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(canonical))
	expectedStoragePath := filepath.Join(
		root,
		hex.EncodeToString(digest[:]),
		"20260826T200000.000000000Z-session-id.jsonl",
	)
	// Assert the repository creates a private, synchronized, versioned session file.
	require.Equal(t, expectedStoragePath, result.StoragePath)
	info, err := os.Stat(result.StoragePath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Equal(t, os.FileMode(0o700), requireDirectoryMode(t, filepath.Dir(result.StoragePath)))
	payload, err := os.ReadFile(result.StoragePath)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(payload), "\n"))
	require.Contains(t, string(payload), `"type":"session_info"`)
	stored, err := repository.Load(t.Context(), "session-id")
	require.NoError(t, err)
	require.Equal(t, canonical, stored.Header.WorkingDirectory)
	require.Equal(t, "release notes", stored.Entries[0].Information.MustGet().Name)
	require.Equal(t, result.StoragePath, stored.StoragePath)
}

// TestCanonicalWorkingDirectoryTreatsSymlinkAsSameProject verifies canonical and symlink paths reopen the same session.
func TestCanonicalWorkingDirectoryTreatsSymlinkAsSameProject(t *testing.T) {
	t.Parallel()

	// Arrange canonical and symlink paths to the same project directory.
	base := t.TempDir()
	project := filepath.Join(base, "project")
	require.NoError(t, os.Mkdir(project, 0o700))
	link := filepath.Join(base, "project-link")
	require.NoError(t, os.Symlink(project, link))
	canonicalProject, err := sessionstore.CanonicalWorkingDirectory(project)
	require.NoError(t, err)
	canonicalLink, err := sessionstore.CanonicalWorkingDirectory(link)
	require.NoError(t, err)
	require.Equal(t, canonicalProject, canonicalLink)

	// Act by persisting through the canonical path and reopening through the symlink path.
	root := filepath.Join(base, "sessions")
	canonicalRepository := sessionstore.New(root, canonicalProject, sessionfilesystem.New())
	require.NoError(t, canonicalRepository.Initialize(t.Context()))
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	_, err = canonicalRepository.Append(t.Context(), hostsessions.AppendCommand{
		Header:      session.Header{Version: 1, ID: "shared-id", CreatedAt: createdAt, WorkingDirectory: canonicalProject},
		StoragePath: "",
		Entry: session.Entry{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](), ID: "entry-id", CreatedAt: createdAt, Information: mo.Some(session.Information{Name: "shared project"}), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost]()},
	})
	require.NoError(t, err)
	symlinkRepository := sessionstore.New(root, canonicalLink, sessionfilesystem.New())
	require.NoError(t, symlinkRepository.Initialize(t.Context()))
	loaded, err := symlinkRepository.Load(t.Context(), "shared-id")

	// Assert both paths address the same stored session.
	require.NoError(t, err)
	require.Equal(t, session.ID("shared-id"), loaded.Header.ID)
}

// TestActiveSessionCreatesNoFileBeforeNaming verifies initialization remains memory-only.
func TestActiveSessionCreatesNoFileBeforeNaming(t *testing.T) {
	t.Parallel()

	// Arrange an empty repository and deterministic active-session identity.
	root := filepath.Join(t.TempDir(), "sessions")
	project, err := sessionstore.CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := sessionstore.New(root, project, sessionfilesystem.New())
	controller := gomock.NewController(t)
	ids := hostsessions.NewMockIDGenerator(controller)
	clock := hostsessions.NewMockClock(controller)
	ids.EXPECT().NewID().Return("startup-id", nil)
	clock.EXPECT().Now().Return(time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC))
	active := hostsessions.New(repository, ids, clock, nil, project)

	// Act by initializing the active session without assigning a name.
	require.NoError(t, active.Initialize(t.Context()))
	projects, err := os.ReadDir(root)

	// Assert project directories contain no session file and storage path remains absent.
	require.NoError(t, err)
	require.Len(t, projects, 1)
	files, err := os.ReadDir(filepath.Join(root, projects[0].Name()))
	require.NoError(t, err)
	require.Empty(t, files)
	require.False(t, active.ActiveInfo().StoragePath.IsPresent())
}

// TestRepositoryReopensListsKnownSessionAndRejectsUnknownID verifies reopening retains one known session and rejects an unknown ID.
func TestRepositoryReopensListsKnownSessionAndRejectsUnknownID(t *testing.T) {
	t.Parallel()

	// Arrange an initialized repository and one append command for a known session.
	root := filepath.Join(t.TempDir(), "sessions")
	project, err := sessionstore.CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := sessionstore.New(root, project, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	command := hostsessions.AppendCommand{
		Header:      session.Header{Version: 1, ID: "known-id", CreatedAt: createdAt, WorkingDirectory: project},
		StoragePath: "",
		Entry: session.Entry{
			ID: "entry-id", CreatedAt: createdAt.Add(time.Minute),
			Information: mo.Some(session.Information{Name: "known session"}),
			User:        mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.None[session.ExtensionEnvelope](),
		},
	}

	// Act by appending, reopening, loading, listing, and requesting an unknown session.
	created, err := repository.Append(t.Context(), command)
	require.NoError(t, err)
	_, err = repository.Append(t.Context(), command)
	require.Error(t, err)
	reopened := sessionstore.New(root, project, sessionfilesystem.New())
	require.NoError(t, reopened.Initialize(t.Context()))
	loaded, err := reopened.Load(t.Context(), "known-id")
	require.NoError(t, err)
	require.Equal(t, created.StoragePath, loaded.StoragePath)
	listed, err := reopened.List(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, session.ID("known-id"), listed[0].Header.ID)
	_, err = reopened.Load(t.Context(), "unknown-id")

	// Assert the known session survives reopening and the unknown identifier is rejected.
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestRepositoryReopensNameLargerThanScannerToken verifies framing retains a name beyond the scanner token limit.
func TestRepositoryReopensNameLargerThanScannerToken(t *testing.T) {
	t.Parallel()

	// Arrange a session name larger than the default scanner token limit.
	root := filepath.Join(t.TempDir(), "sessions")
	project, err := sessionstore.CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := sessionstore.New(root, project, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	largeName := strings.Repeat("n", bufio.MaxScanTokenSize+1024)
	// Act by appending the large name and loading it from a reopened repository.
	_, err = repository.Append(t.Context(), hostsessions.AppendCommand{
		Header:      session.Header{Version: 1, ID: "large-name", CreatedAt: createdAt, WorkingDirectory: project},
		StoragePath: "",
		Entry: session.Entry{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](), ID: "entry-id", CreatedAt: createdAt.Add(time.Second), Information: mo.Some(session.Information{Name: largeName}), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost]()},
	})
	require.NoError(t, err)
	reopened := sessionstore.New(root, project, sessionfilesystem.New())
	require.NoError(t, reopened.Initialize(t.Context()))
	loaded, err := reopened.Load(t.Context(), "large-name")

	// Assert the complete name survives record framing and reconstruction.
	require.NoError(t, err)
	require.Equal(t, largeName, loaded.Entries[0].Information.MustGet().Name)
}

// TestAppendRejectsStoragePathOutsideProjectDirectory verifies an outside sentinel file cannot become session storage.
func TestAppendRejectsStoragePathOutsideProjectDirectory(t *testing.T) {
	t.Parallel()

	// Arrange an initialized repository and a sentinel file outside its project directory.
	base := t.TempDir()
	canonical, err := sessionstore.CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := sessionstore.New(filepath.Join(base, "sessions"), canonical, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	outsidePath := filepath.Join(base, "outside.jsonl")
	require.NoError(t, os.WriteFile(outsidePath, []byte("sentinel"), 0o600))
	// Act by attempting to append through the outside storage path.
	_, err = repository.Append(t.Context(), hostsessions.AppendCommand{
		Header:      session.Header{Version: 1, ID: "session-id", CreatedAt: time.Now(), WorkingDirectory: canonical},
		StoragePath: outsidePath,
		Entry: session.Entry{
			ID: "entry-id", CreatedAt: time.Now(), Information: mo.Some(session.Information{Name: "name"}),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.None[session.ExtensionEnvelope](),
		},
	})

	// Assert the append fails and leaves the sentinel file unchanged.
	require.Error(t, err)
	content, readErr := os.ReadFile(outsidePath)
	require.NoError(t, readErr)
	require.Equal(t, "sentinel", string(content))
}

func requireDirectoryMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}
