//go:build !integration

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

// TestCloneModelResponseClonesMutableOptionValues verifies output snapshots isolate mutable provider and argument bytes.
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
					Arguments: testToolCallArguments(`{"items":["first"]}`),
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
	clonedArguments := clonedCall.Arguments.Bytes()
	clonedArguments[0] = '['

	assert.Equal(t, byte(1), original.Content[0].ProviderContext.OrEmpty().Payload[0])
	assert.Equal(t, `{"items":["first"]}`, original.Content[1].ToolCall.OrEmpty().Arguments.String())
	assert.True(t, cloned.Content[0].ToolCall.IsNone())
	assert.True(t, cloned.Content[1].ProviderContext.IsNone())
}

// TestProjectHistoryOrdersStoredAndSkippedResultsByModelCallOrder verifies call order, skipped results,
// and clone ownership.
func TestProjectHistoryOrdersStoredAndSkippedResultsByModelCallOrder(t *testing.T) {
	t.Parallel()

	// Arrange ordered model calls and stored-result combinations that include missing and unexpected results.
	calls := []model.ToolCall{
		{ID: "call-a", Name: "tool-a", Arguments: testToolCallArguments(`{}`)},
		{ID: "call-b", Name: "tool-b", Arguments: testToolCallArguments(`{}`)},
		{ID: "call-c", Name: "tool-c", Arguments: testToolCallArguments(`{}`)},
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
			projected := ProjectHistory(history)

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

// TestProjectHistoryCollectsResponseResultsAcrossInterveningMessages verifies tool-result ownership does not depend
// on storage adjacency and projection remains stable when applied repeatedly.
func TestProjectHistoryCollectsResponseResultsAcrossInterveningMessages(t *testing.T) {
	t.Parallel()

	// Arrange two calls whose persisted results surround model-visible extension messages and arrive out of call order.
	history := []agent.HistoryEntry{
		testHistoryModelEntry("call-a", "call-b"),
		testHistoryUserEntry("extension first"),
		testHistoryResultEntry("call-b", "stored-b"),
		testHistoryUserEntry("extension second"),
		testHistoryResultEntry("call-a", "stored-a"),
	}

	// Act by projecting persisted history twice.
	projected := ProjectHistory(history)
	reprojected := ProjectHistory(projected)

	// Assert actual results follow call order before the intervening messages, with no synthetic duplicate.
	require.Len(t, projected, 5)
	assert.Equal(t, agent.HistoryEntryModel, projected[0].Kind)
	assert.Equal(t, "call-a", projected[1].ToolResult.MustGet().CallID)
	assert.Equal(t, "stored-a", projected[1].ToolResult.MustGet().Contents[0].Text.MustGet())
	assert.Equal(t, "call-b", projected[2].ToolResult.MustGet().CallID)
	assert.Equal(t, "stored-b", projected[2].ToolResult.MustGet().Contents[0].Text.MustGet())
	assert.Equal(t, "extension first", projected[3].User.MustGet().Text(""))
	assert.Equal(t, "extension second", projected[4].User.MustGet().Text(""))
	assert.Equal(t, projected, reprojected)
}

// TestProjectHistoryScopesRepeatedCallIDsToTheirModelResponse verifies a later response delimits result ownership.
func TestProjectHistoryScopesRepeatedCallIDsToTheirModelResponse(t *testing.T) {
	t.Parallel()

	// Arrange two responses that reuse one call ID while only the later response owns an actual result.
	history := []agent.HistoryEntry{
		testHistoryModelEntry("same"),
		testHistoryUserEntry("between responses"),
		testHistoryModelEntry("same"),
		testHistoryResultEntry("same", "second actual"),
	}

	// Act by projecting response-local tool ownership.
	projected := ProjectHistory(history)

	// Assert the first response gets a skipped result and the later response retains its own actual result.
	require.Len(t, projected, 5)
	first := projected[1].ToolResult.MustGet()
	assert.Equal(t, "same", first.CallID)
	assert.True(t, first.IsError)
	assert.Contains(t, first.Contents[0].Text.MustGet(), "skipped")
	assert.Equal(t, "between responses", projected[2].User.MustGet().Text(""))
	assert.Equal(t, agent.HistoryEntryModel, projected[3].Kind)
	second := projected[4].ToolResult.MustGet()
	assert.Equal(t, "same", second.CallID)
	assert.False(t, second.IsError)
	assert.Equal(t, "second actual", second.Contents[0].Text.MustGet())
}

// TestProjectHistorySkipsMissingSelectedPayload verifies malformed history variants do not become zero entries.
func TestProjectHistorySkipsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	projected := ProjectHistory([]agent.HistoryEntry{{
		Kind:       agent.HistoryEntryModel,
		User:       mo.None[model.Message](),
		Model:      mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}})

	assert.Empty(t, projected)
}

// testHistoryModelEntry creates one finalized tool-use response for history projection tests.
func testHistoryModelEntry(callIDs ...string) agent.HistoryEntry {
	content := make([]model.Content, 0, len(callIDs))
	for _, callID := range callIDs {
		content = append(content, model.Content{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall: mo.Some(model.ToolCall{
				ID: callID, Name: "tool-" + callID, Arguments: testToolCallArguments(`{}`),
			}),
		})
	}
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(model.Response{
			Content: content, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
		ToolResult: mo.None[agent.ToolResult](),
	}
}

// testHistoryUserEntry creates one model-visible intervening message for history projection tests.
func testHistoryUserEntry(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}

// testHistoryResultEntry creates one persisted actual tool result for history projection tests.
func testHistoryResultEntry(callID, text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: callID, ToolName: "tool-" + callID, Contents: tool.TextContents(text), IsError: false,
		}),
	}
}
