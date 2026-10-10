//go:build !integration

package sessions

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestCompactedHistoryUsesLatestSummaryAndExactRetainedSuffix verifies successive compaction and original history.
func TestCompactedHistoryUsesLatestSummaryAndExactRetainedSuffix(t *testing.T) {
	t.Parallel()

	// Arrange two compactions around original messages and a complete tool-call group.
	entries := []session.Entry{
		newCompactionUserEntry("u1", mo.None[string](), "old"),
		compactionModelEntry("m1", "u1", "call"),
		compactionToolEntry("r1", "m1", "call"),
		newCompactionUserEntry("u2", mo.Some("r1"), "retained before second compaction"),
		newCompactionMarkerEntry("c1", "u2", "first summary", "m1"),
		newCompactionUserEntry("u3", mo.Some("c1"), "new after first compaction"),
		newCompactionMarkerEntry("c2", "u3", "latest summary", "u2"),
		newCompactionUserEntry("u4", mo.Some("c2"), "new after second compaction"),
	}

	// Act by projecting model context and independent original client history.
	contextHistory := compactedHistoryFromEntries(entries)
	clientHistory := historyFromEntries(entries)

	// Assert only the latest summary and its exact suffix enter model context.
	require.Len(t, contextHistory, 4)
	require.Contains(t, contextHistory[0].User.MustGet().Text(""), "latest summary")
	require.Equal(t, "retained before second compaction", contextHistory[1].User.MustGet().Text(""))
	require.Equal(t, "new after first compaction", contextHistory[2].User.MustGet().Text(""))
	require.Equal(t, "new after second compaction", contextHistory[3].User.MustGet().Text(""))

	// Assert every original model-visible record remains available to client history without compaction summaries.
	require.Len(t, clientHistory, 6)
	require.Equal(t, "old", clientHistory[0].User.MustGet().Text(""))
	require.Equal(t, agent.HistoryEntryModel, clientHistory[1].Kind)
	require.Equal(t, agent.HistoryEntryToolResult, clientHistory[2].Kind)
}

// TestCompactedHistoryKeepsCompleteToolGroupAtBoundary verifies a model tool call retains all associated results.
func TestCompactedHistoryKeepsCompleteToolGroupAtBoundary(t *testing.T) {
	t.Parallel()

	// Arrange a compaction whose first retained entry is a model response with one tool call.
	entries := []session.Entry{
		newCompactionUserEntry("u1", mo.None[string](), "summarized"),
		compactionModelEntry("m1", "u1", "call"),
		compactionToolEntry("r1", "m1", "call"),
		newCompactionMarkerEntry("c1", "r1", "summary", "m1"),
	}

	// Act by projecting compacted model context.
	history := compactedHistoryFromEntries(entries)

	// Assert the synthetic summary is followed by the whole call/result group.
	require.Len(t, history, 3)
	require.Equal(t, agent.HistoryEntryUser, history[0].Kind)
	require.Equal(t, agent.HistoryEntryModel, history[1].Kind)
	require.Equal(t, agent.HistoryEntryToolResult, history[2].Kind)
	require.Equal(t, "call", history[2].ToolResult.MustGet().CallID)
}

// compactionModelEntry returns one model response containing a finalized tool call.
func compactionModelEntry(id, parentID, callID string) session.Entry {
	entry := newCompactionBaseEntry(id, mo.Some(parentID))
	entry.Model = mo.Some(model.Response{
		Content: []model.Content{{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.Some(model.ToolCall{ID: callID, Name: "tool", Arguments: testToolCallArguments(`{}`)}),
		}},
		Outcome:       mo.Some(model.OutcomeToolUse),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.Some(model.ProviderID("provider")),
		Model:         mo.Some(model.ID("model")),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	})
	return entry
}

// compactionToolEntry returns one terminal result for a retained tool call.
func compactionToolEntry(id, parentID, callID string) session.Entry {
	entry := newCompactionBaseEntry(id, mo.Some(parentID))
	entry.ToolResult = mo.Some(agent.ToolResult{
		CallID: callID, ToolName: "tool", Contents: nil, IsError: false,
	})
	return entry
}
