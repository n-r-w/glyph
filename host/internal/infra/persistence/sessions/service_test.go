package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

func TestModelTextRecordContainsOnlyOrderedCompletedText(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	encoded, err := encodeEntry(session.Entry{
		ID: "model-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
		User: mo.None[model.Message](),
		Model: mo.Some(model.Response{
			Content: []model.Content{
				{
					Kind: model.ContentText, Text: mo.Some("first"), Final: true,
					ProviderContext: mo.Some(model.ProviderContext{
						Source: model.ProviderContextSource{
							ProviderID: "provider", API: "responses", Model: "model",
							CompatibilityKey: mo.Some("key"),
						},
						Payload: []byte{1, 2, 3},
					}),
					ToolCall: mo.None[model.ToolCall](),
				},
				{
					Kind: model.ContentText, Text: mo.Some("second"), Final: true,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				},
			},
			Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.Some("must not persist"),
			Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
			ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
	})
	require.NoError(t, err)

	var record map[string]any
	require.NoError(t, json.Unmarshal(encoded, &record))
	require.Equal(t, map[string]any{
		"type": "model_text", "id": "model-entry", "createdAt": createdAt.Format(time.RFC3339Nano),
		"content": []any{"first", "second"},
	}, record)

	decoded, err := decodeEntry(encoded)
	require.NoError(t, err)
	response := decoded.Model.MustGet()
	require.Equal(t, mo.Some(model.OutcomeStop), response.Outcome)
	require.Equal(t, mo.None[string](), response.ErrorMessage)
	require.Len(t, response.Content, 2)
}

func TestNameAppendOrdersModeWriteSyncCloseBeforeActiveMutation(t *testing.T) {
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
	repositoryMock := hostsessions.NewMockRepository(controller)
	active := hostsessions.New(repositoryMock, ids, clock, project)
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
	info, err := active.SetActiveName(t.Context(), "ordered")
	require.NoError(t, err)
	require.Equal(t, []string{"open", "mode", "write", "sync", "close", "repository return"}, steps)
	require.Equal(t, mo.Some("ordered"), info.Name)
	require.True(t, info.StoragePath.IsPresent())
}

func TestNameAppendFailuresPreserveActiveState(t *testing.T) {
	t.Parallel()

	for _, stage := range []string{"open", "mode", "write", "sync", "close"} {
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
			active := hostsessions.New(repository, ids, clock, project)
			createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
			ids.EXPECT().NewID().Return("session-id", nil)
			clock.EXPECT().Now().Return(createdAt)
			require.NoError(t, active.Initialize(t.Context()))
			before := active.ActiveInfo()
			ids.EXPECT().NewID().Return("entry-id", nil)
			clock.EXPECT().Now().Return(createdAt.Add(time.Second))
			expectInitialAppendFailure(t, stage, repository, fileSystem, file)
			_, err = active.SetActiveName(t.Context(), "must not commit")
			require.Error(t, err)
			require.Equal(t, before, active.ActiveInfo())
		})
	}
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
	case "open":
		open().Return(nil, errors.New("open failed"))
	case "mode":
		gomock.InOrder(
			open().Return(file, nil),
			file.EXPECT().Chmod(os.FileMode(fileMode)).Return(errors.New("mode failed")),
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
