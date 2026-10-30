//go:build !integration

package extension

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// requestEncodingSuite checks public payload precedence and malformed content boundaries.
type requestEncodingSuite struct {
	// Suite provides the test context for these scenarios.
	suite.Suite
}

// TestCompactionRequestEncoding checks selected public entries and model-content rejection.
func TestCompactionRequestEncoding(t *testing.T) {
	t.Parallel()
	// Arrange independent conversion scenarios.
	// Act and Assert through the suite.
	suite.Run(t, &requestEncodingSuite{Suite: suite.Suite{}})
}

// TestPreparedCorrespondence checks source metadata survives public composition.
func (s *requestEncodingSuite) TestPreparedCorrespondence() {
	t := s.T()
	// Arrange prepared user content from an original source.
	entry := newEncodingTestEntry()
	entry.History.Kind = agent.HistoryEntryUser
	entry.History.User = mo.Some(model.TextMessage("prepared text"))
	// Act through source-bearing request mappings.
	wires, err := MapCompactionRequestEntries(
		[]CompactionRequestEntry{{Entry: entry, PrivateBytes: 12, EstimatedTokens: 9}},
	)
	require.NoError(t, err)
	decoded, err := decodePolicyRequestEntries(wires)
	// Assert content, correspondence and extension estimates survive together.
	require.NoError(t, err)
	require.Equal(t, entry, decoded[0].Entry)
	require.Equal(t, int64(12), decoded[0].PrivateBytes)
	require.Equal(t, int64(9), decoded[0].EstimatedTokens)
}

// TestMalformedConversation checks exact errors from the selected model and tool conversions.
func (s *requestEncodingSuite) TestMalformedConversation() {
	t := s.T()
	// Arrange unsupported content kinds in each conversation alternative.
	entry := newEncodingTestEntry()
	entry.History.Kind = agent.HistoryEntryModel
	entry.History.Model = mo.Some(model.Response{
		Content: []model.Content{
			{
				Kind:            0,
				Text:            mo.None[string](),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		Outcome:       mo.None[model.Outcome](),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	})
	// Act and Assert the model converter's reachable error.
	_, err := MapPreparedCompactionHistory(session.PreparedHistory{entry})
	require.EqualError(t, err, "unknown compaction model content kind 0")
	entry.History.Kind = agent.HistoryEntryToolResult
	entry.History.ToolResult = mo.Some(
		agent.ToolResult{
			CallID:   "",
			ToolName: "",
			Contents: []tool.ResultContent{{Kind: 0, Text: mo.None[string](), Image: mo.None[tool.ResultImage]()}},
			IsError:  false,
		},
	)
	_, err = MapPreparedCompactionHistory(session.PreparedHistory{entry})
	require.EqualError(t, err, "unknown compaction tool content kind 0")
}

// TestModelPayloadBoundaries checks kind-dependent missing, ambiguous, and valid payloads.
func (s *requestEncodingSuite) TestModelPayloadBoundaries() {
	t := s.T()
	// Arrange each public content kind and every presence combination.
	kinds := []extensionpb.SessionTreeModelContentKind{
		extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT,
		extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL,
		extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING,
		extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL,
	}
	for _, kind := range kinds {
		for _, textPresent := range []bool{false, true} {
			for _, callPresent := range []bool{false, true} {
				content := new(extensionpb.SessionTreeModelContent)
				content.SetKind(kind)
				if textPresent {
					content.SetText("")
				}
				if callPresent {
					content.SetToolCall(
						extensionpb.SessionTreeToolCall_builder{
							Id:            nil,
							Name:          nil,
							ArgumentsJson: []byte(`{}`),
						}.Build(),
					)
				}
				// Act through the shared public decoder.
				decoded, err := DecodeSessionTreeModelContents([]*extensionpb.SessionTreeModelContent{content})
				// Assert exact rejection or complete finalized content with absent provider context.
				if kind == extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL {
					if !callPresent || textPresent {
						require.EqualError(t, err, "model tool-call payload is missing or ambiguous")
						continue
					}
				} else if !textPresent || callPresent {
					require.EqualError(t, err, "model text payload is missing or ambiguous")
					continue
				}
				require.NoError(t, err)
				require.Len(t, decoded, 1)
				require.True(t, decoded[0].Final)
				require.True(t, decoded[0].ProviderContext.IsNone())
				require.Equal(t, decodeRequestContentKind(kind), decoded[0].Kind)
			}
		}
	}
	_, err := DecodeSessionTreeModelContents([]*extensionpb.SessionTreeModelContent{nil})
	require.EqualError(t, err, "model content payload is missing")
	for _, kind := range []extensionpb.SessionTreeModelContentKind{extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED, 99} {
		_, err = DecodeSessionTreeModelContents(
			[]*extensionpb.SessionTreeModelContent{
				extensionpb.SessionTreeModelContent_builder{Kind: new(kind), Text: nil, ToolCall: nil}.Build(),
			},
		)
		require.EqualError(t, err, "model content kind is unspecified")
	}
	content := extensionpb.SessionTreeModelContent_builder{
		Kind:     new(extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL),
		Text:     nil,
		ToolCall: extensionpb.SessionTreeToolCall_builder{Id: nil, Name: nil, ArgumentsJson: []byte(`broken`)}.Build(),
	}.Build()
	_, err = DecodeSessionTreeModelContents([]*extensionpb.SessionTreeModelContent{content})
	require.ErrorContains(t, err, "validate tool call arguments:")
}

// newEncodingTestEntry supplies a prepared record for conversion scenarios.
func newEncodingTestEntry() session.PreparedHistoryEntry {
	return session.PreparedHistoryEntry{
		History: agent.HistoryEntry{
			Kind:       0,
			User:       mo.None[model.Message](),
			Model:      mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
		},
		SourceID:        "source",
		SourcePosition:  3,
		SourceKind:      session.HistorySourceUser,
		PreviousSummary: false,
	}
}
