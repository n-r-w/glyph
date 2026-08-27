package programmatic

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

func TestMapSessionEntriesProjectsToolContinuationWithoutProviderContext(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentReasoning, Text: mo.None[string](), Final: true,
				ProviderContext: mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("key"),
					},
					Payload: []byte{1, 2, 3},
				}),
				ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentRefusal, Text: mo.Some("visible refusal"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
			},
		},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
		Usage:       mo.Some(model.Usage{}),
		Diagnostics: []model.Diagnostic{{Code: "later", Message: "must not project"}},
	}
	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name,
		Contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: mo.Some("result"), Image: mo.None[tool.ResultImage]()},
			{
				Kind: tool.ResultContentImage, Text: mo.None[string](),
				Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}),
			},
		},
		IsError: false,
	}
	entries := []session.Entry{
		{
			ID: "model-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
			User: mo.None[session.UserMessage](), Model: mo.Some(response), ToolResult: mo.None[session.ToolResult](),
		},
		{
			ID: "tool-entry", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](), ToolResult: mo.Some(result),
		},
	}

	mapped := mapSessionEntries(entries)

	require.Len(t, mapped, 2)
	require.Equal(t, controller.HistoryEntryModel, mapped[0].Kind)
	publicResponse := mapped[0].Model.MustGet()
	require.Equal(t, mo.Some(controller.ModelOutcomeToolUse), publicResponse.Outcome)
	require.Equal(t, mo.Some("response-id"), publicResponse.ResponseID)
	require.True(t, publicResponse.Usage.IsPresent())
	require.Empty(t, publicResponse.Diagnostics)
	require.Len(t, publicResponse.Content, 1)
	require.Equal(t, call.ID, publicResponse.Content[0].ToolCall.MustGet().CallID)
	require.Equal(t, 3, publicResponse.Content[0].ToolCall.MustGet().Position)
	require.Equal(t, controller.HistoryEntryToolResult, mapped[1].Kind)
	publicResult := mapped[1].ToolResult.MustGet()
	require.Equal(t, result.CallID, publicResult.CallID)
	require.Len(t, publicResult.Contents, 1)
	require.Equal(t, "result", publicResult.Contents[0].Text.MustGet())
}

func TestMapHistoryProjectsTerminalToolResults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		contents        []tool.ResultContent
		expectedEntries int
		expectedText    []string
	}{
		{name: "nil remains public", contents: nil, expectedEntries: 1, expectedText: nil},
		{name: "non-nil empty remains public", contents: []tool.ResultContent{}, expectedEntries: 1, expectedText: nil},
		{name: "image-only is omitted", contents: []tool.ResultContent{{
			Kind: tool.ResultContentImage, Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}),
		}}, expectedEntries: 0, expectedText: nil},
		{name: "mixed keeps ordered text", contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: mo.Some("first"), Image: mo.None[tool.ResultImage]()},
			{
				Kind: tool.ResultContentImage, Text: mo.None[string](),
				Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}),
			},
			{Kind: tool.ResultContentText, Text: mo.Some("second"), Image: mo.None[tool.ResultImage]()},
		}, expectedEntries: 1, expectedText: []string{"first", "second"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			history := []agent.HistoryEntry{{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "tool", Contents: test.contents, IsError: true,
				}),
			}}

			mapped, err := mapHistory(history)

			require.NoError(t, err)
			require.Len(t, mapped, test.expectedEntries)
			if test.expectedEntries == 0 {
				return
			}
			result := mapped[0].ToolResult.MustGet()
			require.Equal(t, "call", result.CallID)
			require.Equal(t, "tool", result.ToolName)
			require.True(t, result.IsError)
			require.Len(t, result.Contents, len(test.expectedText))
			for index := range test.expectedText {
				require.Equal(t, test.expectedText[index], result.Contents[index].Text.MustGet())
			}
		})
	}
}
