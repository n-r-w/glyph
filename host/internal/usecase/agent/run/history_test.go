package run

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestCloneMessageClonesImageBytesInsideOption verifies cloned input images do not share mutable data.
func TestCloneMessageClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	original := model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](),
		MediaType: mo.Some("image/png"), Data: mo.Some([]byte{1, 2, 3}),
	}}}

	cloned := original.Clone()
	clonedData, ok := cloned.Content[0].Data.Get()
	require.True(t, ok)
	clonedData[0] = 9
	originalData, ok := original.Content[0].Data.Get()
	require.True(t, ok)

	assert.Equal(t, byte(1), originalData[0])
	assert.True(t, cloned.Content[0].Text.IsNone())
	assert.Equal(t, "image/png", cloned.Content[0].MediaType.OrEmpty())
}

// TestCloneModelResponseClonesMutableOptionValues verifies output snapshots isolate provider bytes and tool arguments.
func TestCloneModelResponseClonesMutableOptionValues(t *testing.T) {
	t.Parallel()

	original := model.Response{
		Content: []model.Content{
			{
				Kind:  model.ContentReasoning,
				Text:  mo.Some("reason"),
				Final: false,
				ProviderContext: mo.Some(model.ProviderContext{
					Source:  model.ProviderContextSource{},
					Payload: []byte{1, 2, 3},
				}),
				ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall: mo.Some(model.ToolCall{
					ID:        "",
					Name:      "",
					Arguments: map[string]any{"items": []any{"first"}},
				}),
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
	}

	cloned := original.Clone()
	clonedContext := cloned.Content[0].ProviderContext.OrEmpty()
	clonedContext.Payload[0] = 9
	clonedCall := cloned.Content[1].ToolCall.OrEmpty()
	clonedCall.Arguments["items"].([]any)[0] = "changed"

	assert.Equal(t, byte(1), original.Content[0].ProviderContext.OrEmpty().Payload[0])
	assert.Equal(t, "first", original.Content[1].ToolCall.OrEmpty().Arguments["items"].([]any)[0])
	assert.True(t, cloned.Content[0].ToolCall.IsNone())
	assert.True(t, cloned.Content[1].ProviderContext.IsNone())
}

// TestProjectHistoryOrdersStoredAndSkippedResultsByModelCallOrder verifies call order, skipped results,
// and clone ownership.
func TestProjectHistoryOrdersStoredAndSkippedResultsByModelCallOrder(t *testing.T) {
	t.Parallel()

	// Arrange ordered model calls and stored-result combinations that include missing and unexpected results.
	calls := []model.ToolCall{
		{ID: "call-a", Name: "tool-a", Arguments: map[string]any{}},
		{ID: "call-b", Name: "tool-b", Arguments: map[string]any{}},
		{ID: "call-c", Name: "tool-c", Arguments: map[string]any{}},
	}
	modelContent := make([]model.Content, 0, len(calls))
	for _, call := range calls {
		modelContent = append(modelContent, model.Content{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
		})
	}
	cases := []struct {
		name      string
		storedIDs []string
	}{
		{name: "all results stored", storedIDs: []string{"call-a", "call-b", "call-c"}},
		{name: "all results missing", storedIDs: nil},
		{name: "stored prefix with missing suffix", storedIDs: []string{"call-a", "call-b"}},
		{name: "missing prefix with stored suffix", storedIDs: []string{"call-b", "call-c"}},
		{name: "interior missing result", storedIDs: []string{"call-a", "call-c"}},
		{name: "unexpected result is omitted", storedIDs: []string{"call-a", "unexpected", "call-b", "call-c"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			history := []agent.HistoryEntry{{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(model.Response{
					Content: modelContent, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
				ToolResult: mo.None[agent.ToolResult](),
			}}
			stored := make(map[string]bool, len(test.storedIDs))
			for _, callID := range test.storedIDs {
				stored[callID] = true
				history = append(history, agent.HistoryEntry{
					Kind:  agent.HistoryEntryToolResult,
					User:  mo.None[model.Message](),
					Model: mo.None[model.Response](),
					ToolResult: mo.Some(agent.ToolResult{
						CallID: callID, ToolName: "stored-tool", Contents: tool.TextContents("stored-" + callID),
						IsError: false,
					}),
				})
			}

			// Act by projecting stored history into provider-visible call order.
			projected := projectHistory(history)

			// Assert stored results keep their values, missing results become skipped, and unexpected results are omitted.
			require.Len(t, projected, len(calls)+1)
			for index, call := range calls {
				result := projected[index+1].ToolResult.MustGet()
				assert.Equal(t, call.ID, result.CallID)
				if stored[call.ID] {
					assert.False(t, result.IsError)
					assert.Equal(t, "stored-"+call.ID, result.Contents[0].Text.MustGet())
					continue
				}
				assert.Equal(t, call.Name, result.ToolName)
				assert.True(t, result.IsError)
				assert.Contains(t, result.Contents[0].Text.MustGet(), "skipped")
			}
			for index := 1; index < len(projected); index++ {
				result := projected[index].ToolResult.MustGet()
				if !stored[result.CallID] {
					continue
				}
				result.Contents[0].Text = mo.Some("mutated")
				for historyIndex := 1; historyIndex < len(history); historyIndex++ {
					original := history[historyIndex].ToolResult.MustGet()
					if original.CallID == result.CallID {
						assert.Equal(t, "stored-"+result.CallID, original.Contents[0].Text.MustGet())
					}
				}
				break
			}
		})
	}
}

// TestProjectHistorySkipsMissingSelectedPayload verifies malformed history variants do not become zero entries.
func TestProjectHistorySkipsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	projected := projectHistory([]agent.HistoryEntry{{
		Kind:       agent.HistoryEntryModel,
		User:       mo.None[model.Message](),
		Model:      mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}})

	assert.Empty(t, projected)
}
