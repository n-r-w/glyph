package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

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
				Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
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
				Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
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
				}), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
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
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
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
