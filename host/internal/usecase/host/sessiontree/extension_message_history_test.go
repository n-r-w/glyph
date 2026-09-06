//go:build !integration

package sessiontree

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestHistoryProjectsBothExtensionMessageVisibilities verifies client presentation does not affect model context.
func TestHistoryProjectsBothExtensionMessageVisibilities(t *testing.T) {
	t.Parallel()

	// Arrange one hidden checkpoint and two exact model-visible messages.
	entries := []session.Entry{
		extensionHistoryEntry("checkpoint", mo.Some(session.ExtensionEnvelope{
			ExtensionID: "example", EntryType: "state", Data: []byte(`{ "step": 2 }`),
		}), mo.None[session.ExtensionMessage]()),
		extensionHistoryEntry("visible", mo.None[session.ExtensionEnvelope](), mo.Some(session.ExtensionMessage{
			ExtensionID: "example",
			EntryType:   "note",
			Text:        "visible\ntext",
			Visibility:  session.ClientVisibilityVisible,
		})),
		extensionHistoryEntry("hidden", mo.None[session.ExtensionEnvelope](), mo.Some(session.ExtensionMessage{
			ExtensionID: "example", EntryType: "note", Text: "hidden text", Visibility: session.ClientVisibilityHidden,
		})),
	}

	// Act by projecting active session entries to provider-neutral history.
	history := HistoryFromEntries(entries)

	// Assert only model-visible messages enter history as exact user text in order.
	require.Len(t, history, 2)
	require.Equal(t, agent.HistoryEntryUser, history[0].Kind)
	require.Equal(t, "visible\ntext", history[0].User.MustGet().Text("\n"))
	require.Equal(t, "hidden text", history[1].User.MustGet().Text("\n"))
}

// extensionHistoryEntry creates one extension-owned history projection fixture.
func extensionHistoryEntry(
	id string,
	envelope mo.Option[session.ExtensionEnvelope],
	message mo.Option[session.ExtensionMessage],
) session.Entry {
	return session.Entry{
		ID: id, ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: envelope, ExtensionMessage: message,
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}
