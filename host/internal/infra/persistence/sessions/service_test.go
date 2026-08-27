package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// TestTerminalModelAndToolResultRecordsRoundTripContinuationData verifies terminal identity and continuation data persist.
func TestTerminalModelAndToolResultRecordsRoundTripContinuationData(t *testing.T) {
	t.Parallel()

	// Arrange terminal model content, provider context, tool identity, usage, and tool output.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	call := model.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: mo.Some("before tool"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.None[string](), Final: true,
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
		Extension: mo.None[session.ExtensionEnvelope](),
	}
	// Act by round-tripping terminal model and tool-result records through JSONL.
	encodedModel, err := encodeEntry(modelEntry)
	require.NoError(t, err)
	// Assert identity, outcomes, usage, provider context, and tool output survive exactly.
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
		Extension: mo.None[session.ExtensionEnvelope](),
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
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			}
			encoded, encodeErr := encodeEntry(entry)
			require.NoError(t, encodeErr)
			decoded, decodeErr := decodeEntry(encoded)
			require.NoError(t, decodeErr)
			require.Equal(t, entry, decoded)
		})
	}
}

// TestFullContentRecordsRoundTrip verifies every durable content kind survives one compact JSONL record.
func TestFullContentRecordsRoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange one entry for each durable content family.
	createdAt := time.Date(2026, 8, 27, 2, 0, 0, 0, time.UTC)
	providerContext := model.ProviderContext{
		Source: model.ProviderContextSource{
			ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("compatible"),
		},
		Payload: []byte{0, 1, 2, 255},
	}
	tests := []struct {
		name  string
		entry session.Entry
		check func(*testing.T, []byte, session.Entry)
	}{
		{
			name: "ordered user text and image",
			entry: session.Entry{
				ID: "user-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
				User: mo.Some(model.Message{Content: []model.InputContent{
					{Kind: model.InputContentText, Text: mo.Some("before"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
					{Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some([]byte{0, 10, 255})},
					{Kind: model.InputContentText, Text: mo.Some("after"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
				}}),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
				Extension: mo.None[session.ExtensionEnvelope](),
			},
			check: func(t *testing.T, encoded []byte, decoded session.Entry) {
				t.Helper()
				require.Len(t, decoded.User.MustGet().Content, 3)
				require.Equal(t, "image/png", decoded.User.MustGet().Content[1].MediaType.MustGet())
				require.Equal(t, []byte{0, 10, 255}, decoded.User.MustGet().Content[1].Data.MustGet())
				require.Equal(t, 1, bytes.Count(encoded, []byte{'\n'}))
			},
		},
		{
			name: "visible model content diagnostics and opaque context",
			entry: session.Entry{
				ID: "model-entry", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](),
				Model: mo.Some(model.Response{
					Content: []model.Content{
						{Kind: model.ContentRefusal, Text: mo.Some("refused"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
						{Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true, ProviderContext: mo.Some(providerContext), ToolCall: mo.None[model.ToolCall]()},
					},
					Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
					Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
					ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
					Usage:       mo.None[model.Usage](),
					Diagnostics: []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
				}),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			},
			check: func(t *testing.T, _ []byte, decoded session.Entry) {
				t.Helper()
				response := decoded.Model.MustGet()
				require.Equal(t, "visible reasoning", response.Content[1].Text.MustGet())
				require.Equal(t, []byte{0, 1, 2, 255}, response.Content[1].ProviderContext.MustGet().Payload)
				require.Equal(t, []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}}, response.Diagnostics)
			},
		},
		{
			name: "ordered tool result image",
			entry: session.Entry{
				ID: "tool-entry", CreatedAt: createdAt.Add(2 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "render", IsError: false,
					Contents: []tool.ResultContent{
						{Kind: tool.ResultContentText, Text: mo.Some("preview"), Image: mo.None[tool.ResultImage]()},
						{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{MediaType: "image/webp", Data: []byte{3, 2, 1, 0}})},
					},
				}),
				Extension: mo.None[session.ExtensionEnvelope](),
			},
			check: func(t *testing.T, _ []byte, decoded session.Entry) {
				t.Helper()
				image := decoded.ToolResult.MustGet().Contents[1].Image.MustGet()
				require.Equal(t, "image/webp", image.MediaType)
				require.Equal(t, []byte{3, 2, 1, 0}, image.Data)
			},
		},
		{
			name: "compact extension JSON",
			entry: session.Entry{
				ID: "extension-entry", CreatedAt: createdAt.Add(3 * time.Second),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
				Extension: mo.Some(session.ExtensionEnvelope{
					ExtensionID: "example.extension", EntryType: "checkpoint",
					Data: []byte("{\n  \"text\": \"line\\nvalue\", \"items\": [1, 2]\n}"),
				}),
			},
			check: func(t *testing.T, encoded []byte, decoded session.Entry) {
				t.Helper()
				require.Equal(t, 1, bytes.Count(encoded, []byte{'\n'}))
				require.JSONEq(t, `{ "text": "line\nvalue", "items": [1, 2] }`, string(decoded.Extension.MustGet().Data))
			},
		},
	}

	// Act by encoding and decoding every entry through one JSONL record.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := encodeEntry(test.entry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)

			// Assert identity, content, framing, and opaque bytes survive exactly.
			require.NoError(t, err)
			require.Equal(t, test.entry.ID, decoded.ID)
			test.check(t, encoded, decoded)
		})
	}
}

// TestEncodeModelContentShape verifies encoding accepts only provider-neutral terminal content shapes.
func TestEncodeModelContentShape(t *testing.T) {
	t.Parallel()

	// Arrange all content kinds and all text, context, and tool-call presence masks.
	kinds := []model.ContentKind{
		model.ContentText, model.ContentToolCall, model.ContentReasoning, model.ContentRefusal,
	}

	// Act by validating each domain content shape during encoding.
	for _, kind := range kinds {
		for mask := range 8 {
			name := fmt.Sprintf("%s/%03b", modelContentKindName(kind), mask)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				content := modelContentShape(kind, mask)
				_, err := encodeModelContent(&content)

				// Assert only the shape allowed for this content kind succeeds.
				if validModelContentShape(kind, mask) {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
			})
		}
	}
}

// TestDecodeModelContentShape verifies JSONL decoding rejects every invalid terminal content shape.
func TestDecodeModelContentShape(t *testing.T) {
	t.Parallel()

	// Arrange every JSONL field-presence mask for each model content kind.
	kinds := []model.ContentKind{
		model.ContentText, model.ContentToolCall, model.ContentReasoning, model.ContentRefusal,
	}

	// Act by decoding a strict JSONL model record for each shape.
	for _, kind := range kinds {
		for mask := range 8 {
			name := fmt.Sprintf("%s/%03b", modelContentKindName(kind), mask)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				line, err := encodeLine(modelRecord{
					Type: "model", ID: "entry", CreatedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano),
					Response: modelResponseRecord{
						Content: []modelContentRecord{modelContentRecordShape(kind, mask)},
						Outcome: model.OutcomeStop, ErrorMessage: nil, Provider: nil, Model: nil,
						ResponseModel: nil, ResponseID: nil, Usage: nil, Diagnostics: nil,
					},
				})
				require.NoError(t, err)
				_, err = decodeEntry(line)

				// Assert only the shape allowed for this content kind succeeds.
				if validModelContentShape(kind, mask) {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
			})
		}
	}
}

func modelContentShape(kind model.ContentKind, mask int) model.Content {
	content := model.Content{
		Kind: kind, Text: mo.None[string](), Final: true,
		ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
	}
	if mask&1 != 0 {
		content.Text = mo.Some("visible")
	}
	if mask&2 != 0 {
		content.ProviderContext = mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.None[string](),
			},
			Payload: []byte("opaque"),
		})
	}
	if mask&4 != 0 {
		content.ToolCall = mo.Some(model.ToolCall{
			ID: "call", Name: "tool", Arguments: map[string]any{"key": "value"},
		})
	}
	return content
}

func modelContentRecordShape(kind model.ContentKind, mask int) modelContentRecord {
	record := modelContentRecord{Kind: kind, Text: nil, ProviderContext: nil, ToolCall: nil}
	if mask&1 != 0 {
		record.Text = new("visible")
	}
	if mask&2 != 0 {
		record.ProviderContext = &providerContextRecord{
			ProviderID: "provider", API: "responses", Model: "model",
			CompatibilityKey: nil, Payload: []byte("opaque"),
		}
	}
	if mask&4 != 0 {
		record.ToolCall = &toolCallRecord{
			ID: "call", Name: "tool", Arguments: map[string]any{"key": "value"},
		}
	}
	return record
}

func validModelContentShape(kind model.ContentKind, mask int) bool {
	switch kind {
	case model.ContentText, model.ContentRefusal:
		return mask == 1
	case model.ContentToolCall:
		return mask == 4
	case model.ContentReasoning:
		return mask == 1 || mask == 2 || mask == 3
	default:
		return false
	}
}

func modelContentKindName(kind model.ContentKind) string {
	switch kind {
	case model.ContentText:
		return "text"
	case model.ContentToolCall:
		return "tool_call"
	case model.ContentReasoning:
		return "reasoning"
	case model.ContentRefusal:
		return "refusal"
	default:
		return "unknown"
	}
}

// TestImageByteSliceStateRoundTrip verifies nil, empty, and nonempty image bytes keep their slice state.
func TestImageByteSliceStateRoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange nil, non-nil empty, and nonempty image byte slices.
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "nil", data: nil},
		{name: "non-nil empty", data: []byte{}},
		{name: "nonempty", data: []byte{1, 2, 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			createdAt := time.Date(2026, 8, 27, 3, 0, 0, 0, time.UTC)
			userEntry := session.Entry{
				ID: "user-image", CreatedAt: createdAt, Information: mo.None[session.Information](),
				User: mo.Some(model.Message{Content: []model.InputContent{{
					Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"),
					Data: mo.Some(test.data),
				}}}),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
				Extension: mo.None[session.ExtensionEnvelope](),
			}
			// Act by round-tripping user and tool-result images through JSONL.
			encoded, err := encodeEntry(userEntry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)

			// Assert byte values and nil versus non-nil slice state are exact.
			require.NoError(t, err)
			userData := decoded.User.MustGet().Content[0].Data.MustGet()
			require.Equal(t, test.data, userData)
			require.Equal(t, test.data == nil, userData == nil)

			toolEntry := session.Entry{
				ID: "tool-image", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "render", IsError: false,
					Contents: []tool.ResultContent{{
						Kind: tool.ResultContentImage, Text: mo.None[string](),
						Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: test.data}),
					}},
				}),
				Extension: mo.None[session.ExtensionEnvelope](),
			}
			encoded, err = encodeEntry(toolEntry)
			require.NoError(t, err)
			decoded, err = decodeEntry(encoded)
			require.NoError(t, err)
			toolData := decoded.ToolResult.MustGet().Contents[0].Image.MustGet().Data
			require.Equal(t, test.data, toolData)
			require.Equal(t, test.data == nil, toolData == nil)
		})
	}
}

// TestToolResultContentsSliceStateRoundTrip verifies JSONL preserves nil, empty, and ordered result content.
func TestToolResultContentsSliceStateRoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange nil, empty, and populated tool-result content slices.
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

	// Act by encoding and decoding each content-slice state.
	for _, test := range contentsCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entry := session.Entry{
				ID: "tool-entry", CreatedAt: time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "tool", Contents: test.contents, IsError: false,
				}), Extension: mo.None[session.ExtensionEnvelope](),
			}

			encoded, err := encodeEntry(entry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)

			// Assert round-trip decoding preserves the exact slice state.
			require.NoError(t, err)
			actual := decoded.ToolResult.MustGet().Contents
			require.Equal(t, test.contents, actual)
			require.Equal(t, test.contents == nil, actual == nil)
		})
	}
}

// TestProviderContextPayloadSliceStateRoundTrip verifies JSONL preserves nil, empty, and opaque provider payloads.
func TestProviderContextPayloadSliceStateRoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange nil, empty, and opaque provider-context payloads.
	payloadCases := []struct {
		name    string
		payload []byte
	}{
		{name: "nil", payload: nil},
		{name: "non-nil empty", payload: []byte{}},
		{name: "opaque bytes", payload: []byte{0, 1, 255}},
	}

	// Act by encoding and decoding each provider-context payload state.
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
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			}

			encoded, err := encodeEntry(entry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)

			// Assert round-trip decoding preserves payload presence and bytes.
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
