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

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

func TestTerminalModelAndToolResultRecordsRoundTripContinuationData(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	call := model.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: mo.Some("before tool"), Final: true,
				ProviderContext: mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID: "provider", API: "responses", Model: "model",
						CompatibilityKey: mo.Some("key"),
					},
					Payload: []byte{0, 1, 2, 255},
				}),
				ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
			},
		},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
		Usage: mo.Some(model.Usage{
			InputTokens: 10, OutputTokens: 20, CachedInputTokens: 3,
			CacheWriteTokens: 4, ReasoningTokens: 5, TotalTokens: 37,
		}),
		Diagnostics: nil,
	}
	modelEntry := session.Entry{
		ID: "model-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.Some(response), ToolResult: mo.None[session.ToolResult](),
	}
	encodedModel, err := encodeEntry(modelEntry)
	require.NoError(t, err)
	var modelRecord map[string]any
	require.NoError(t, json.Unmarshal(encodedModel, &modelRecord))
	require.Equal(t, "model", modelRecord["type"])
	decodedModel, err := decodeEntry(encodedModel)
	require.NoError(t, err)
	require.Equal(t, modelEntry, decodedModel)

	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("tool output"), IsError: false,
	}
	toolEntry := session.Entry{
		ID: "tool-entry", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](), ToolResult: mo.Some(result),
	}
	encodedTool, err := encodeEntry(toolEntry)
	require.NoError(t, err)
	var toolRecord map[string]any
	require.NoError(t, json.Unmarshal(encodedTool, &toolRecord))
	require.Equal(t, "tool_result", toolRecord["type"])
	decodedTool, err := decodeEntry(encodedTool)
	require.NoError(t, err)
	require.Equal(t, toolEntry, decodedTool)

	for _, test := range []struct {
		name       string
		outcome    model.Outcome
		responseID mo.Option[string]
		usage      mo.Option[model.Usage]
	}{
		{name: "aborted with present empty identity and zero usage", outcome: model.OutcomeAborted, responseID: mo.Some(""), usage: mo.Some(model.Usage{})},
		{name: "failed with absent identity and usage", outcome: model.OutcomeFailed, responseID: mo.None[string](), usage: mo.None[model.Usage]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entry := session.Entry{
				ID: "terminal-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](),
				Model: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(test.outcome), ErrorMessage: mo.Some("safe terminal failure"),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
					ResponseID: test.responseID, Usage: test.usage, Diagnostics: nil,
				}),
				ToolResult: mo.None[session.ToolResult](),
			}
			encoded, encodeErr := encodeEntry(entry)
			require.NoError(t, encodeErr)
			decoded, decodeErr := decodeEntry(encoded)
			require.NoError(t, decodeErr)
			require.Equal(t, entry, decoded)
		})
	}
}

func TestToolResultContentsSliceStateRoundTrip(t *testing.T) {
	t.Parallel()

	contentsCases := []struct {
		name     string
		contents []tool.ResultContent
	}{
		{name: "nil", contents: nil},
		{name: "non-nil empty", contents: []tool.ResultContent{}},
		{name: "ordered text", contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: mo.Some(""), Image: mo.None[tool.ResultImage]()},
			{Kind: tool.ResultContentText, Text: mo.Some("second"), Image: mo.None[tool.ResultImage]()},
		}},
	}
	for _, test := range contentsCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entry := session.Entry{
				ID: "tool-entry", CreatedAt: time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "tool", Contents: test.contents, IsError: false,
				}),
			}

			encoded, err := encodeEntry(entry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)
			require.NoError(t, err)

			actual := decoded.ToolResult.MustGet().Contents
			require.Equal(t, test.contents, actual)
			require.Equal(t, test.contents == nil, actual == nil)
		})
	}
}

func TestProviderContextPayloadSliceStateRoundTrip(t *testing.T) {
	t.Parallel()

	payloadCases := []struct {
		name    string
		payload []byte
	}{
		{name: "nil", payload: nil},
		{name: "non-nil empty", payload: []byte{}},
		{name: "opaque bytes", payload: []byte{0, 1, 255}},
	}
	for _, test := range payloadCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entry := session.Entry{
				ID: "model-entry", CreatedAt: time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.Some(model.Response{
					Content: []model.Content{{
						Kind: model.ContentReasoning, Text: mo.None[string](), Final: true,
						ProviderContext: mo.Some(model.ProviderContext{
							Source: model.ProviderContextSource{
								ProviderID: "provider", API: "responses", Model: "model",
								CompatibilityKey: mo.None[string](),
							},
							Payload: test.payload,
						}),
						ToolCall: mo.None[model.ToolCall](),
					}},
					Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
				ToolResult: mo.None[session.ToolResult](),
			}

			encoded, err := encodeEntry(entry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)
			require.NoError(t, err)

			actual := decoded.Model.MustGet().Content[0].ProviderContext.MustGet().Payload
			require.Equal(t, test.payload, actual)
			require.Equal(t, test.payload == nil, actual == nil)
			if len(actual) > 0 {
				actual[0]++
				require.NotEqual(t, test.payload, actual)
			}
		})
	}
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
