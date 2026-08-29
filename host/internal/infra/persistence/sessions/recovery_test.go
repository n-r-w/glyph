package sessions

import (
	"bytes"

	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestInvalidListWarningsRetainSkippedFileContext verifies list warnings include exact path and original cause.
//
//nolint:paralleltest // The test temporarily captures the process-global structured logger.
func TestInvalidListWarningsRetainSkippedFileContext(t *testing.T) {
	// Arrange one candidate path, diagnostic error, and captured structured logger.
	candidatePath := "/user-owned/sessions/candidate.jsonl"
	cause := errors.New("decode session entry record 1: unexpected end of JSON input")
	var output bytes.Buffer
	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))

	// Act by sending the skipped-file failure through the list warning boundary.
	warnUnavailableSession(t.Context(), "list", candidatePath, classifyListWarningDiagnostic(cause), cause)

	// Assert the warning keeps its path, category, and original skipped-file error.
	var warning map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &warning))
	assert.Equal(t, "WARN", warning["level"])
	assert.Equal(t, "session file is unavailable", warning["msg"])
	assert.Equal(t, "list", warning["operation"])
	assert.Equal(t, candidatePath, warning["path"])
	assert.Equal(t, "invalid_session_file", warning["diagnostic"])
	assert.Equal(t, cause.Error(), warning["error"])
	assert.NotContains(t, warning, "session_id")
}

// TestInterruptedTailRecoveryOrdersDurabilityOperations verifies recovery truncates and syncs before close.
func TestInterruptedTailRecoveryOrdersDurabilityOperations(t *testing.T) {
	t.Parallel()

	// Arrange a read-only session and generated filesystem mocks for probe, mode repair, and recovery opens.
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
	require.NoError(t, os.WriteFile(path, []byte("placeholder"), 0o400))
	readOnlyInfo, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0o600))
	recoveryInfo, err := os.Stat(path)
	require.NoError(t, err)
	header := fmt.Sprintf(`{"type":"session","version":1,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n", project)
	entry := `{"type":"session_info","id":"entry-1","createdAt":"2026-08-27T10:00:01Z","name":"Stored"}` + "\n"
	payload := []byte(header + entry + `{"type":"user"`)
	completeSize := int64(len(header + entry))
	readPayload := func(data []byte) func([]byte) (int, error) {
		used := false
		return func(target []byte) (int, error) {
			if used {
				return 0, io.EOF
			}
			used = true
			return copy(target, data), nil
		}
	}
	gomock.InOrder(
		fileSystem.EXPECT().OpenFile(repository.projectDirectory, name, os.O_RDONLY, os.FileMode(0)).Return(probe, nil),
		probe.EXPECT().Stat().Return(readOnlyInfo, nil),
		probe.EXPECT().ReadPayload(gomock.Any()).DoAndReturn(readPayload(payload)),
		probe.EXPECT().ReadPayload(gomock.Any()).Return(0, io.EOF),
		probe.EXPECT().Close().Return(nil),
		fileSystem.EXPECT().OpenFile(repository.projectDirectory, name, os.O_RDONLY, os.FileMode(0)).Return(preparation, nil),
		preparation.EXPECT().Stat().Return(readOnlyInfo, nil),
		preparation.EXPECT().ReadPayload(gomock.Any()).DoAndReturn(readPayload(payload)),
		preparation.EXPECT().ReadPayload(gomock.Any()).Return(0, io.EOF),
		preparation.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
		preparation.EXPECT().Close().Return(nil),
		fileSystem.EXPECT().OpenFile(repository.projectDirectory, name, os.O_RDWR, os.FileMode(0)).Return(recovery, nil),
		recovery.EXPECT().Stat().Return(recoveryInfo, nil),
		recovery.EXPECT().ReadPayload(gomock.Any()).DoAndReturn(readPayload(payload)),
		recovery.EXPECT().ReadPayload(gomock.Any()).Return(0, io.EOF),
		recovery.EXPECT().Truncate(completeSize).Return(nil),
		recovery.EXPECT().Sync().Return(nil),
		recovery.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
		recovery.EXPECT().Close().Return(nil),
	)

	// Act by explicitly loading the session with one interrupted final append.
	loaded, loadErr := repository.Load(t.Context(), session.ID("stored"))

	// Assert only the preceding complete entry is restored after ordered recovery.
	require.NoError(t, loadErr)
	require.Len(t, loaded.Entries, 1)
	assert.Equal(t, "entry-1", loaded.Entries[0].ID)
}

func expectInitialAppendFailure(
	t *testing.T,
	stage string,
	repository *Service,
	fileSystem *MockFileSystem,
	file *MockFile,
) {
	t.Helper()
	open := func() *gomock.Call {
		return fileSystem.EXPECT().OpenFile(
			repository.projectDirectory, gomock.Any(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(fileMode),
		)
	}
	successfulWrite := func(payload []byte) (int, error) { return len(payload), nil }
	switch stage {
	case "permission":
		open().Return(nil, os.ErrPermission)
	case "open":
		open().Return(nil, errors.New("open failed"))
	case "mode":
		gomock.InOrder(
			open().Return(file, nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(errors.New("mode failed")),
			file.EXPECT().Close().Return(nil),
		)
	case "short write":
		gomock.InOrder(
			open().Return(file, nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
			file.EXPECT().WritePayload(gomock.Any()).DoAndReturn(func(payload []byte) (int, error) {
				return len(payload) - 1, nil
			}),
			file.EXPECT().Close().Return(nil),
		)
	case "write":
		gomock.InOrder(
			open().Return(file, nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
			file.EXPECT().WritePayload(gomock.Any()).Return(0, errors.New("write failed")),
			file.EXPECT().Close().Return(nil),
		)
	case "sync":
		gomock.InOrder(
			open().Return(file, nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
			file.EXPECT().WritePayload(gomock.Any()).DoAndReturn(successfulWrite),
			file.EXPECT().Sync().Return(errors.New("sync failed")),
			file.EXPECT().Close().Return(nil),
		)
	case "close":
		gomock.InOrder(
			open().Return(file, nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
			file.EXPECT().WritePayload(gomock.Any()).DoAndReturn(successfulWrite),
			file.EXPECT().Sync().Return(nil),
			file.EXPECT().Close().Return(errors.New("close failed")),
		)
	default:
		t.Fatalf("unknown failure stage %q", stage)
	}
}
