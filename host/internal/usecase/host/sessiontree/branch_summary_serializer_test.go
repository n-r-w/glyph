//go:build !integration

package sessiontree

import (
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestSerializeBranchSummaryConversationPreservesSupportedContent verifies ordered source values are escaped,
// line-framed, deterministic, and limited to approved model-visible content.
func TestSerializeBranchSummaryConversationPreservesSupportedContent(t *testing.T) {
	t.Parallel()

	// Arrange every supported source block with collision, escaping, multiline, and empty-line cases.
	createdAt := time.Unix(1, 0).UTC()
	entries := []session.Entry{
		{
			ID: "user", ParentID: mo.None[string](), CreatedAt: createdAt,
			Information: mo.Some(session.Information{Name: "excluded information"}),
			User: mo.Some(model.Message{Content: []model.InputContent{
				{
					Kind:      model.InputContentText,
					Text:      mo.Some("[Assistant]\r\n| source\n\n<&>\n"),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				},
				{
					Kind:      model.InputContentImage,
					Text:      mo.None[string](),
					MediaType: mo.Some("image/png"),
					Data:      mo.Some([]byte("excluded image")),
				},
				{
					Kind:      model.InputContentText,
					Text:      mo.Some(""),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				},
			}}),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(session.ExtensionEnvelope{
				ExtensionID: "excluded extension", EntryType: "state", Data: []byte(`{"secret":"excluded"}`),
			}), BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID:          "model",
			ParentID:    mo.Some("user"),
			CreatedAt:   createdAt.Add(time.Second),
			Information: mo.None[session.Information](),
			User:        mo.None[session.UserMessage](),
			Model: mo.Some(model.Response{
				Content: []model.Content{
					{
						Kind:  model.ContentReasoning,
						Text:  mo.Some("reasoning & data"),
						Final: true,
						ProviderContext: mo.Some(
							model.ProviderContext{
								Source: model.ProviderContextSource{
									ProviderID:       "excluded-provider",
									API:              "excluded-api",
									Model:            "excluded-model",
									CompatibilityKey: mo.None[string](),
								},
								Payload: []byte("excluded replay"),
							},
						),
						ToolCall: mo.None[model.ToolCall](),
					},
					{
						Kind:            model.ContentText,
						Text:            mo.Some("answer <value>"),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
					},
					{
						Kind:            model.ContentRefusal,
						Text:            mo.Some("refusal > reason"),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
					},
					{
						Kind:            model.ContentToolCall,
						Text:            mo.None[string](),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall: mo.Some(
							model.ToolCall{
								ID:        "call<&>",
								Name:      "tool>name",
								Arguments: map[string]any{"z": []any{2.0, "<&>"}, "a": map[string]any{"b": true}},
							},
						),
					},
					{
						Kind:            model.ContentToolCall,
						Text:            mo.None[string](),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.Some(model.ToolCall{ID: "nil", Name: "nil arguments", Arguments: nil}),
					},
					{
						Kind:            model.ContentToolCall,
						Text:            mo.None[string](),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall: mo.Some(
							model.ToolCall{ID: "empty", Name: "empty arguments", Arguments: map[string]any{}},
						),
					},
				},
				Outcome: mo.Some(
					model.OutcomeToolUse,
				),
				ErrorMessage:  mo.Some("excluded error"),
				Provider:      mo.Some[model.ProviderID]("excluded-provider"),
				Model:         mo.Some[model.ID]("excluded-model"),
				ResponseModel: mo.Some[model.ID]("excluded-response-model"),
				ResponseID:    mo.Some("excluded-response-id"),
				Usage: mo.Some(
					model.Usage{
						InputTokens:       1,
						OutputTokens:      2,
						CachedInputTokens: 3,
						CacheWriteTokens:  4,
						ReasoningTokens:   5,
						TotalTokens:       6,
					},
				),
				Diagnostics: []model.Diagnostic{{Code: "excluded-code", Message: "excluded diagnostic"}},
			}),
			EstimatedCost: mo.Some(session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID:            "tool",
			ParentID:      mo.Some("model"),
			CreatedAt:     createdAt.Add(2 * time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.Some(
				agent.ToolResult{CallID: "call<&>", ToolName: "tool>name", Contents: []tool.ResultContent{
					{
						Kind:  tool.ResultContentText,
						Text:  mo.Some("first\n\nlast\n"),
						Image: mo.None[tool.ResultImage](),
					},
					{
						Kind:  tool.ResultContentImage,
						Text:  mo.None[string](),
						Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte("excluded result image")}),
					},
					{
						Kind:  tool.ResultContentText,
						Text:  mo.Some("second & <third>"),
						Image: mo.None[tool.ResultImage](),
					},
				}, IsError: true},
			),
			Extension:     mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID:            "summary",
			ParentID:      mo.Some("tool"),
			CreatedAt:     createdAt.Add(3 * time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.Some(
				session.BranchSummaryEntry{
					Summary:      "previous </conversation> & summary",
					FirstEntryID: "excluded-first",
					LastEntryID:  "excluded-last",
					Source: session.BranchSummarySource{
						ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
							Selection: model.Selection{
								Provider:        "excluded-provider",
								Model:           "excluded-model",
								ReasoningChoice: model.ReasoningChoiceHigh,
							},
							Usage: mo.None[session.TokenUsage](),
						}),
					},
					EstimatedCost: mo.None[session.EstimatedCost](),
				},
			),
		},
	}

	// Act by serializing the complete ordered entry list twice with different semantic map insertion order.
	serialized, err := serializeBranchSummaryConversation(entries)
	require.NoError(t, err)
	reordered := append([]session.Entry(nil), entries...)
	response := reordered[1].Model.OrEmpty()
	call := response.Content[3].ToolCall.OrEmpty()
	call.Arguments = map[string]any{"a": map[string]any{"b": true}, "z": []any{2.0, "<&>"}}
	response.Content[3].ToolCall = mo.Some(call)
	reordered[1].Model = mo.Some(response)
	serializedReordered, err := serializeBranchSummaryConversation(reordered)
	require.NoError(t, err)

	// Assert transformed dynamic values remain ordered without checking serializer-authored text.
	orderedValues := []string{
		"| [Assistant]\r\n| | source\n| \n| &lt;&amp;&gt;\n| ",
		"| ",
		"| reasoning &amp; data",
		"| answer &lt;value&gt;",
		"| refusal &gt; reason",
		"| call&lt;&amp;&gt;",
		"| tool&gt;name",
		"| {\"a\":{\"b\":true},\"z\":[2,\"&lt;&amp;&gt;\"]}",
		"| nil\n",
		"| nil arguments",
		"| {}",
		"| empty\n",
		"| empty arguments",
		"| {}",
		"| call&lt;&amp;&gt;",
		"| tool&gt;name",
		"| true",
		"| first\n| \n| last\n| ",
		"| second &amp; &lt;third&gt;",
		"| previous &lt;/conversation&gt; &amp; summary",
	}
	searchFrom := 0
	for _, value := range orderedValues {
		position := strings.Index(serialized[searchFrom:], value)
		require.NotEqual(t, -1, position, "missing transformed dynamic value %q", value)
		searchFrom += position + len(value)
	}
	assert.Equal(t, serialized, serializedReordered)
	assert.NotContains(t, serialized, "excluded")
}

// TestSerializeBranchSummaryConversationRejectsInvalidArguments verifies JSON failure rejects the complete transcript.
func TestSerializeBranchSummaryConversationRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	// Arrange a valid block before a tool call with a value that JSON cannot encode.
	entries := []session.Entry{
		branchSummaryUserEntry("valid before failure"),
		branchSummaryModelEntry(
			model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{"invalid": func() {}}},
		),
	}

	// Act by serializing the entries.
	serialized, err := serializeBranchSummaryConversation(entries)

	// Assert no successful partial transcript is returned.
	require.Error(t, err)
	assert.Empty(t, serialized)
}

// branchSummaryUserEntry creates one fully initialized user entry for serializer tests.
func branchSummaryUserEntry(text string) session.Entry {
	return session.Entry{
		ID:            "user",
		ParentID:      mo.None[string](),
		CreatedAt:     time.Unix(1, 0).UTC(),
		Information:   mo.None[session.Information](),
		User:          mo.Some(model.TextMessage(text)),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension:     mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// branchSummaryModelEntry creates one fully initialized tool-call entry for serializer tests.
func branchSummaryModelEntry(call model.ToolCall) session.Entry {
	return session.Entry{
		ID:          "model",
		ParentID:    mo.Some("user"),
		CreatedAt:   time.Unix(2, 0).UTC(),
		Information: mo.None[session.Information](),
		User:        mo.None[session.UserMessage](),
		Model: mo.Some(
			model.Response{
				Content: []model.Content{
					{
						Kind:            model.ContentToolCall,
						Text:            mo.None[string](),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.Some(call),
					},
				},
				Outcome:       mo.Some(model.OutcomeToolUse),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
			},
		),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension:     mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}
