package sessions

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestInterruptedTailRecoveryFailuresUsePersistenceClassification verifies each recovery durability failure reaches runtime mapping.
func TestInterruptedTailRecoveryFailuresUsePersistenceClassification(t *testing.T) {
	t.Parallel()

	// Arrange the recovery operations that can fail after one valid interrupted-tail scan.
	for _, stage := range []string{"truncate", "sync", "mode", "close"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			fileSystem := NewMockFileSystem(controller)
			probe := NewMockFile(controller)
			preparation := NewMockFile(controller)
			recovery := NewMockFile(controller)
			root := t.TempDir()
			project := t.TempDir()
			repository := New(root, project, fileSystem)
			require.NoError(t, repository.Initialize(t.Context()))
			name := "stored.jsonl"
			path := filepath.Join(repository.projectDirectory, name)
			require.NoError(t, os.WriteFile(path, []byte("placeholder"), fileMode))
			info, err := os.Stat(path)
			require.NoError(t, err)
			header := fmt.Sprintf(`{"type":"session","version":1,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n", project)
			entry := `{"type":"session_info","id":"entry-1","createdAt":"2026-08-27T10:00:01Z","name":"Stored"}` + "\n"
			payload := []byte(header + entry + `{"type":"user"`)
			completeSize := int64(len(header + entry))

			gomock.InOrder(
				fileSystem.EXPECT().OpenFile(repository.projectDirectory, name, os.O_RDONLY, os.FileMode(0)).Return(probe, nil),
				probe.EXPECT().Stat().Return(info, nil),
				probe.EXPECT().ReadPayload(gomock.Any()).DoAndReturn(readFailurePayload(payload)),
				probe.EXPECT().ReadPayload(gomock.Any()).Return(0, io.EOF),
				probe.EXPECT().Close().Return(nil),
				fileSystem.EXPECT().OpenFile(repository.projectDirectory, name, os.O_RDONLY, os.FileMode(0)).Return(preparation, nil),
				preparation.EXPECT().Stat().Return(info, nil),
				preparation.EXPECT().ReadPayload(gomock.Any()).DoAndReturn(readFailurePayload(payload)),
				preparation.EXPECT().ReadPayload(gomock.Any()).Return(0, io.EOF),
				preparation.EXPECT().Close().Return(nil),
				fileSystem.EXPECT().OpenFile(repository.projectDirectory, name, os.O_RDWR, os.FileMode(0)).Return(recovery, nil),
				recovery.EXPECT().Stat().Return(info, nil),
				recovery.EXPECT().ReadPayload(gomock.Any()).DoAndReturn(readFailurePayload(payload)),
				recovery.EXPECT().ReadPayload(gomock.Any()).Return(0, io.EOF),
			)
			expectRecoveryFailure(stage, recovery, completeSize)

			// Act by resuming the stored session through interrupted-tail recovery.
			_, err = repository.Load(t.Context(), session.ID("stored"))

			// Assert the runtime persistence classification retains the injected recovery cause.
			require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
			require.NotErrorIs(t, err, session.ErrUnavailable)
			require.ErrorContains(t, err, stage+" failed")
		})
	}
}

func readFailurePayload(payload []byte) func([]byte) (int, error) {
	return func(target []byte) (int, error) { return copy(target, payload), nil }
}

func expectRecoveryFailure(stage string, file *MockFile, completeSize int64) {
	switch stage {
	case "truncate":
		gomock.InOrder(
			file.EXPECT().Truncate(completeSize).Return(errors.New("truncate failed")),
			file.EXPECT().Close().Return(nil),
		)
	case "sync":
		gomock.InOrder(
			file.EXPECT().Truncate(completeSize).Return(nil),
			file.EXPECT().Sync().Return(errors.New("sync failed")),
			file.EXPECT().Close().Return(nil),
		)
	case "mode":
		gomock.InOrder(
			file.EXPECT().Truncate(completeSize).Return(nil),
			file.EXPECT().Sync().Return(nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(errors.New("mode failed")),
			file.EXPECT().Close().Return(nil),
		)
	case "close":
		gomock.InOrder(
			file.EXPECT().Truncate(completeSize).Return(nil),
			file.EXPECT().Sync().Return(nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
			file.EXPECT().Close().Return(errors.New("close failed")),
		)
	}
}
