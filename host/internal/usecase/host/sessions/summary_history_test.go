//go:build !integration

package sessions

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestHistoryFromEntriesProjectsBranchSummaryAsSyntheticUserContext verifies provider-neutral summary replay and
// extension exclusion.
func TestHistoryFromEntriesProjectsBranchSummaryAsSyntheticUserContext(t *testing.T) {
	t.Parallel()

	// Arrange one model-hidden extension and one persisted branch summary with XML-sensitive text.
	persistedSummary := "already &amp; </summary> <goal>keep output markup</goal>"
	entries := []session.Entry{
		{
			ID:            "extension",
			ParentID:      mo.None[string](),
			CreatedAt:     time.Unix(1, 0).UTC(),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension: mo.Some(
				session.ExtensionEnvelope{ExtensionID: "hidden", EntryType: "state", Data: []byte("secret")},
			),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID: "summary", ParentID: mo.Some("extension"), CreatedAt: time.Unix(2, 0).UTC(),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.Some(session.BranchSummaryEntry{
				Summary: persistedSummary, FirstEntryID: "source-first", LastEntryID: "source-last",
				Source: session.BranchSummarySource{
					ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
						Selection: model.Selection{
							Provider:        "provider",
							Model:           "model",
							ReasoningChoice: model.ReasoningChoiceOff,
						},
						Usage: mo.None[session.TokenUsage](),
					}),
				}, EstimatedCost: mo.None[session.EstimatedCost](),
			}),
		},
	}

	// Act by projecting the active entries into provider history.
	history := sessiontree.HistoryFromEntries(entries)

	// Assert the summary is one synthetic user message and hidden extension data is absent.
	require.Len(t, history, 1)
	assert.Equal(t, agent.HistoryEntryUser, history[0].Kind)
	assert.True(t, history[0].Model.IsNone())
	assert.True(t, history[0].ToolResult.IsNone())
	userMessage, present := history[0].User.Get()
	require.True(t, present)
	require.Len(t, userMessage.Content, 1)
	assert.Equal(t, model.InputContentText, userMessage.Content[0].Kind)
	renderedContext, present := userMessage.Content[0].Text.Get()
	require.True(t, present)

	escapedSummary := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(persistedSummary)
	assert.Contains(t, renderedContext, escapedSummary)
	assert.NotContains(t, renderedContext, persistedSummary)
	assert.Contains(t, html.UnescapeString(renderedContext), persistedSummary)
	assert.NotContains(t, renderedContext, "secret")

	storedSummary, present := entries[1].BranchSummary.Get()
	require.True(t, present)
	assert.Equal(t, persistedSummary, storedSummary.Summary)
}
