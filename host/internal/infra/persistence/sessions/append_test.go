package sessions

import (
	"context"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"

	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// TestNameAppendOrdersModeWriteSyncCloseBeforeActiveMutation verifies durability precedes active publication.
func TestNameAppendOrdersModeWriteSyncCloseBeforeActiveMutation(t *testing.T) {
	t.Parallel()

	// Arrange the active service and ordered filesystem expectations.
	controller := gomock.NewController(t)
	fileSystem := NewMockFileSystem(controller)
	file := NewMockFile(controller)
	ids := hostsessions.NewMockIDGenerator(controller)
	clock := hostsessions.NewMockClock(controller)
	root := filepath.Join(t.TempDir(), "sessions")
	project, err := CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := New(root, project, fileSystem)
	repositoryMock := hostsessions.NewMockRepository(controller)
	active := hostsessions.New(repositoryMock, ids, clock, nil, project)
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Second)
	repositoryMock.EXPECT().Initialize(gomock.Any()).DoAndReturn(repository.Initialize)
	ids.EXPECT().NewID().Return("session-id", nil)
	clock.EXPECT().Now().Return(createdAt)
	require.NoError(t, active.Initialize(t.Context()))

	steps := make([]string, 0, 6)
	gomock.InOrder(
		ids.EXPECT().NewID().Return("entry-id", nil),
		clock.EXPECT().Now().Return(updatedAt),
		repositoryMock.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, command hostsessions.AppendCommand) (hostsessions.AppendResult, error) {
				result, appendErr := repository.Append(ctx, command)
				steps = append(steps, "repository return")
				return result, appendErr
			},
		),
		fileSystem.EXPECT().OpenFile(repository.projectDirectory, gomock.Any(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(fileMode)).DoAndReturn(
			func(string, string, int, os.FileMode) (File, error) {
				steps = append(steps, "open")
				return file, nil
			},
		),
		file.EXPECT().Chmod(os.FileMode(fileMode)).DoAndReturn(func(os.FileMode) error {
			steps = append(steps, "mode")
			return nil
		}),
		file.EXPECT().WritePayload(gomock.Any()).DoAndReturn(func(payload []byte) (int, error) {
			steps = append(steps, "write")
			require.Equal(t, 2, strings.Count(string(payload), "\n"))
			require.Contains(t, string(payload), `"type":"session"`)
			require.Contains(t, string(payload), `"type":"session_info"`)
			return len(payload), nil
		}),
		file.EXPECT().Sync().DoAndReturn(func() error {
			steps = append(steps, "sync")
			return nil
		}),
		file.EXPECT().Close().DoAndReturn(func() error {
			steps = append(steps, "close")
			return nil
		}),
	)
	// Act by assigning the first durable session name.
	info, err := active.SetActiveName(t.Context(), "ordered")

	// Assert mode, write, sync, and close finish before active state changes.
	require.NoError(t, err)
	require.Equal(t, []string{"open", "mode", "write", "sync", "close", "repository return"}, steps)
	require.Equal(t, mo.Some("ordered"), info.Name)
	require.True(t, info.StoragePath.IsPresent())
}

// TestNameAppendFailuresPreserveActiveState verifies each initial-write failure keeps the active snapshot unchanged.
func TestNameAppendFailuresPreserveActiveState(t *testing.T) {
	t.Parallel()

	// Arrange each filesystem failure stage as an independent scenario.
	for _, stage := range []string{"permission", "open", "mode", "short write", "write", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			fileSystem := NewMockFileSystem(controller)
			file := NewMockFile(controller)
			ids := hostsessions.NewMockIDGenerator(controller)
			clock := hostsessions.NewMockClock(controller)
			root := filepath.Join(t.TempDir(), "sessions")
			project, err := CanonicalWorkingDirectory(t.TempDir())
			require.NoError(t, err)
			repository := New(root, project, fileSystem)
			active := hostsessions.New(repository, ids, clock, nil, project)
			createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
			ids.EXPECT().NewID().Return("session-id", nil)
			clock.EXPECT().Now().Return(createdAt)
			require.NoError(t, active.Initialize(t.Context()))
			before := active.ActiveInfo()
			ids.EXPECT().NewID().Return("entry-id", nil)
			clock.EXPECT().Now().Return(createdAt.Add(time.Second))
			expectInitialAppendFailure(t, stage, repository, fileSystem, file)

			// Act by assigning a name through the failing append stage.
			_, err = active.SetActiveName(t.Context(), "must not commit")

			// Assert the error leaves active identity and storage unchanged.
			require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
			require.Equal(t, before, active.ActiveInfo())
		})
	}
}
