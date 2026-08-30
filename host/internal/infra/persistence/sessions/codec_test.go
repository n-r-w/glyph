package sessions

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
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

// TestTerminalModelAndToolResultRecordsRoundTripContinuationData verifies terminal identity and continuation data
// persist.
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
		ParentID:      mo.None[string](),
		ID:            "model-entry",
		CreatedAt:     createdAt,
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.Some(response),
		ToolResult:    mo.None[session.ToolResult](),
		Extension:     mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
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
		ParentID:      mo.None[string](),
		ID:            "tool-entry",
		CreatedAt:     createdAt.Add(time.Second),
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.None[session.ModelResponse](),
		ToolResult:    mo.Some(result),
		Extension:     mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
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
		{
			name:       "aborted with present empty identity and zero usage",
			outcome:    model.OutcomeAborted,
			responseID: mo.Some(""),
			usage:      mo.Some(model.Usage{}),
		},
		{
			name:       "failed with absent identity and usage",
			outcome:    model.OutcomeFailed,
			responseID: mo.None[string](),
			usage:      mo.None[model.Usage](),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entry := session.Entry{
				ParentID:    mo.None[string](),
				ID:          "terminal-entry",
				CreatedAt:   createdAt,
				Information: mo.None[session.Information](),
				User:        mo.None[session.UserMessage](),
				Model: mo.Some(model.Response{
					Content:       nil,
					Outcome:       mo.Some(test.outcome),
					ErrorMessage:  mo.Some("safe terminal failure"),
					Provider:      mo.None[model.ProviderID](),
					Model:         mo.None[model.ID](),
					ResponseModel: mo.None[model.ID](),
					ResponseID:    test.responseID,
					Usage:         test.usage,
					Diagnostics:   nil,
				}),
				ToolResult:    mo.None[session.ToolResult](),
				Extension:     mo.None[session.ExtensionEnvelope](),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.BranchSummaryEntry](),
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
				ParentID:    mo.None[string](),
				ID:          "user-entry",
				CreatedAt:   createdAt,
				Information: mo.None[session.Information](),
				User: mo.Some(model.Message{Content: []model.InputContent{
					{
						Kind:      model.InputContentText,
						Text:      mo.Some("before"),
						MediaType: mo.None[string](),
						Data:      mo.None[[]byte](),
					},
					{
						Kind:      model.InputContentImage,
						Text:      mo.None[string](),
						MediaType: mo.Some("image/png"),
						Data:      mo.Some([]byte{0, 10, 255}),
					},
					{
						Kind:      model.InputContentText,
						Text:      mo.Some("after"),
						MediaType: mo.None[string](),
						Data:      mo.None[[]byte](),
					},
				}}),
				Model:         mo.None[session.ModelResponse](),
				ToolResult:    mo.None[session.ToolResult](),
				Extension:     mo.None[session.ExtensionEnvelope](),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.BranchSummaryEntry](),
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
				ParentID:    mo.None[string](),
				ID:          "model-entry",
				CreatedAt:   createdAt.Add(time.Second),
				Information: mo.None[session.Information](),
				User:        mo.None[session.UserMessage](),
				Model: mo.Some(model.Response{
					Content: []model.Content{
						{
							Kind:            model.ContentRefusal,
							Text:            mo.Some("refused"),
							Final:           true,
							ProviderContext: mo.None[model.ProviderContext](),
							ToolCall:        mo.None[model.ToolCall](),
						},
						{
							Kind:            model.ContentReasoning,
							Text:            mo.Some("visible reasoning"),
							Final:           true,
							ProviderContext: mo.Some(providerContext),
							ToolCall:        mo.None[model.ToolCall](),
						},
					},
					Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
					Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
					ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
					Usage:       mo.None[model.Usage](),
					Diagnostics: []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
				}),
				ToolResult:    mo.None[session.ToolResult](),
				Extension:     mo.None[session.ExtensionEnvelope](),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.BranchSummaryEntry](),
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
				ParentID:    mo.None[string](),
				ID:          "tool-entry",
				CreatedAt:   createdAt.Add(2 * time.Second),
				Information: mo.None[session.Information](),
				User:        mo.None[session.UserMessage](),
				Model:       mo.None[session.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "render", IsError: false,
					Contents: []tool.ResultContent{
						{Kind: tool.ResultContentText, Text: mo.Some("preview"), Image: mo.None[tool.ResultImage]()},
						{
							Kind:  tool.ResultContentImage,
							Text:  mo.None[string](),
							Image: mo.Some(tool.ResultImage{MediaType: "image/webp", Data: []byte{3, 2, 1, 0}}),
						},
					},
				}),
				Extension:     mo.None[session.ExtensionEnvelope](),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.BranchSummaryEntry](),
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
				ParentID:    mo.None[string](),
				ID:          "extension-entry",
				CreatedAt:   createdAt.Add(3 * time.Second),
				Information: mo.None[session.Information](),
				User:        mo.None[session.UserMessage](),
				Model:       mo.None[session.ModelResponse](),
				ToolResult:  mo.None[session.ToolResult](),
				Extension: mo.Some(session.ExtensionEnvelope{
					ExtensionID: "example.extension", EntryType: "checkpoint",
					Data: []byte("{\n  \"text\": \"line\\nvalue\", \"items\": [1, 2]\n}"),
				}),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.BranchSummaryEntry](),
			},
			check: func(t *testing.T, encoded []byte, decoded session.Entry) {
				t.Helper()
				require.Equal(t, 1, bytes.Count(encoded, []byte{'\n'}))
				require.JSONEq(
					t,
					`{ "text": "line\nvalue", "items": [1, 2] }`,
					string(decoded.Extension.MustGet().Data),
				)
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

// TestApplyEncodingFailureDoesNotAccessFilesystem verifies invalid extension JSON stops before storage.
func TestApplyEncodingFailureDoesNotAccessFilesystem(t *testing.T) {
	t.Parallel()

	// Arrange a repository with a generated filesystem mock and one invalid extension envelope.
	fileSystem := NewMockFileSystem(gomock.NewController(t))
	repository := New(t.TempDir(), t.TempDir(), fileSystem)
	createdAt := time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC)
	command := hostsessions.ApplyCommand{
		Header: session.Header{
			Version:          2,
			ID:               "session",
			CreatedAt:        createdAt,
			WorkingDirectory: repository.workingDirectory,
		},
		StoragePath: "",
		Mutation: hostsessions.Mutation{
			Entry: mo.Some(
				session.Entry{
					ParentID:      mo.None[string](),
					ID:            "entry",
					CreatedAt:     createdAt,
					Information:   mo.None[session.Information](),
					User:          mo.None[session.UserMessage](),
					Model:         mo.None[session.ModelResponse](),
					ToolResult:    mo.None[session.ToolResult](),
					EstimatedCost: mo.None[session.EstimatedCost](),
					Extension: mo.Some(
						session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "item", Data: []byte("{")},
					),
					BranchSummary: mo.None[session.BranchSummaryEntry](),
				},
			),
			Navigation:         mo.None[hostsessions.NavigationMutation](),
			Label:              mo.None[hostsessions.LabelMutation](),
			SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
		},
	}

	// Act by applying an entry that cannot be encoded as compact JSONL.
	_, err := repository.Apply(t.Context(), command)

	// Assert encoding fails before the mock filesystem receives any call.
	require.ErrorContains(t, err, "invalid extension entry")
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
					Type: "model", ID: "entry", ParentID: mo.None[string](),
					CreatedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano),
					Response: modelResponseRecord{
						Content: []modelContentRecord{modelContentRecordShape(kind, mask)},
						Outcome: model.OutcomeStop, ErrorMessage: nil, Provider: nil, Model: nil,
						ResponseModel: nil, ResponseID: nil, Usage: nil, Diagnostics: nil,
					},
					EstimatedCost: nil,
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
