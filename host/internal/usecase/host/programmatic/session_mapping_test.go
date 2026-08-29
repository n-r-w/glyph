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

// TestInvalidStoredModelProjectionIsReported verifies invalid stored model content returns a mapping error.
func TestInvalidStoredModelProjectionIsReported(t *testing.T) {
	t.Parallel()

	// Arrange history with a refusal that illegally carries provider context.
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(invalidStoredModelResponse()), ToolResult: mo.None[agent.ToolResult](),
	}}

	// Act by projecting stored history to the public Programmatic model.
	mapped, err := mapHistory(history)

	// Assert the invalid model response is reported and no partial result escapes.
	require.Error(t, err)
	require.Nil(t, mapped)
}

func invalidStoredModelResponse() model.Response {
	return model.Response{
		Content: []model.Content{{
			Kind: model.ContentRefusal, Text: mo.Some("refusal"), Final: true,
			ProviderContext: mo.Some(model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.None[string](),
				},
				Payload: []byte("opaque"),
			}),
			ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
}

// TestMapSessionEntriesProjectsCompletePublicContentWithoutPrivateData verifies public restoration excludes private data.
func TestMapSessionEntriesProjectsCompletePublicContentWithoutPrivateData(t *testing.T) {
	t.Parallel()

	// Arrange complete public content and one private extension entry.
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
		Diagnostics: []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
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
	user := model.Message{Content: []model.InputContent{
		{Kind: model.InputContentText, Text: mo.Some("before"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
		{Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some([]byte{4, 5, 6})},
		{Kind: model.InputContentText, Text: mo.Some("after"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
	}}
	entries := []session.Entry{
		{ParentID: mo.None[string](), ID: "user-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
			User: mo.Some(user), Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{ParentID: mo.None[string](), ID: "model-entry", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
			User: mo.None[session.UserMessage](), Model: mo.Some(response), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{ParentID: mo.None[string](), ID: "tool-entry", CreatedAt: createdAt.Add(2 * time.Second), Information: mo.None[session.Information](),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](), ToolResult: mo.Some(result),
			Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{ParentID: mo.None[string](), ID: "extension-entry", CreatedAt: createdAt.Add(3 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.Some(session.ExtensionEnvelope{
				ExtensionID: "example.extension", EntryType: "checkpoint", Data: []byte(`{"private":true}`),
			}),
			EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
	}

	// Act by projecting durable entries to public session entries.
	mapped, err := mapSessionEntries(entries)

	// Assert ordered public content is complete and private data is omitted.
	require.NoError(t, err)
	require.Len(t, mapped, 3)
	require.Equal(t, controller.HistoryEntryUser, mapped[0].Kind)
	require.True(t, mapped[0].User.IsPresent())
	publicUser := mapped[0].User.MustGet()
	require.Equal(t, user, publicUser)
	require.Equal(t, controller.HistoryEntryModel, mapped[1].Kind)
	publicResponse := mapped[1].Model.MustGet()
	require.Equal(t, mo.Some(controller.ModelOutcomeToolUse), publicResponse.Outcome)
	require.Equal(t, mo.Some("response-id"), publicResponse.ResponseID)
	require.True(t, publicResponse.Usage.IsPresent())
	require.Equal(t, []controller.ModelDiagnostic{{Code: "notice", Message: "safe diagnostic"}}, publicResponse.Diagnostics)
	require.Len(t, publicResponse.Content, 3)
	require.Equal(t, controller.ModelResponseContentReasoning, publicResponse.Content[0].Kind)
	require.Equal(t, "visible reasoning", publicResponse.Content[0].Text.MustGet())
	require.Equal(t, controller.ModelResponseContentRefusal, publicResponse.Content[1].Kind)
	require.Equal(t, "visible refusal", publicResponse.Content[1].Text.MustGet())
	require.Equal(t, call.ID, publicResponse.Content[2].ToolCall.MustGet().CallID)
	require.Equal(t, 3, publicResponse.Content[2].ToolCall.MustGet().Position)
	require.Equal(t, controller.HistoryEntryToolResult, mapped[2].Kind)
	publicResult := mapped[2].ToolResult.MustGet()
	require.Equal(t, result.CallID, publicResult.CallID)
	require.Len(t, publicResult.Contents, 2)
	require.Equal(t, "result", publicResult.Contents[0].Text.MustGet())
	require.Equal(t, []byte{1, 2}, publicResult.Contents[1].Image.MustGet().Data)
}

// TestMapHistoryProjectsTerminalToolResults verifies every valid tool-result content state remains public.
func TestMapHistoryProjectsTerminalToolResults(t *testing.T) {
	t.Parallel()

	// Arrange nil, empty, image-only, and ordered mixed tool-result content.
	cases := []struct {
		name     string
		contents []tool.ResultContent
	}{
		{name: "nil remains public", contents: nil},
		{name: "non-nil empty remains public", contents: []tool.ResultContent{}},
		{name: "image-only remains public", contents: []tool.ResultContent{{
			Kind: tool.ResultContentImage, Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}),
		}}},
		{name: "mixed preserves content order", contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: mo.Some("first"), Image: mo.None[tool.ResultImage]()},
			{
				Kind: tool.ResultContentImage, Text: mo.None[string](),
				Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}),
			},
			{Kind: tool.ResultContentText, Text: mo.Some("second"), Image: mo.None[tool.ResultImage]()},
		}},
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

			// Act by projecting provider history to Programmatic history.
			mapped, err := mapHistory(history)

			// Assert result identity, error state, content order, and image bytes.
			require.NoError(t, err)
			require.Len(t, mapped, 1)
			result := mapped[0].ToolResult.MustGet()
			require.Equal(t, "call", result.CallID)
			require.Equal(t, "tool", result.ToolName)
			require.True(t, result.IsError)
			require.Len(t, result.Contents, len(test.contents))
			for index := range test.contents {
				switch test.contents[index].Kind {
				case tool.ResultContentText:
					require.Equal(t, test.contents[index].Text.MustGet(), result.Contents[index].Text.MustGet())
				case tool.ResultContentImage:
					require.Equal(t, test.contents[index].Image.MustGet().MediaType, result.Contents[index].Image.MustGet().MediaType)
					require.Equal(t, test.contents[index].Image.MustGet().Data, result.Contents[index].Image.MustGet().Data)
				}
			}
		})
	}
}
