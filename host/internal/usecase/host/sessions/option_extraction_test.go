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

// TestTerminalContinuationMissingPayload verifies selected payload absence is diagnosed before a later valid empty payload.
func TestTerminalContinuationMissingPayload(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		kind      agent.HistoryEntryKind
		diagnosis string
	}{
		{name: "user", kind: agent.HistoryEntryUser, diagnosis: "user history payload is missing"},
		{name: "model", kind: agent.HistoryEntryModel, diagnosis: "model history payload is missing"},
		{name: "tool", kind: agent.HistoryEntryToolResult, diagnosis: "tool-result history payload is missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a selected history kind with no corresponding value.
			history := agent.HistoryEntry{
				Kind:       test.kind,
				User:       mo.None[model.Message](),
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
			}

			// Act at the projection boundary; Append separately validates its owned input.
			entry, durable, err := terminalContinuationEntry(history)

			// Assert explicit absence and continued projection of a present zero value.
			require.ErrorContains(t, err, test.diagnosis)
			require.False(t, durable)
			require.Equal(t, session.Entry{}, entry)
			switch test.kind {
			case agent.HistoryEntryUser:
				history.User = mo.Some(model.Message{})
			case agent.HistoryEntryModel:
				history.Model = mo.Some(model.Response{})
			case agent.HistoryEntryToolResult:
				history.ToolResult = mo.Some(agent.ToolResult{})
			}
			entry, durable, err = terminalContinuationEntry(history)
			require.NoError(t, err)
			require.Equal(t, test.kind != agent.HistoryEntryModel, durable)
			require.Equal(t, test.kind == agent.HistoryEntryUser, entry.User.IsSome())
			require.Equal(t, test.kind == agent.HistoryEntryToolResult, entry.ToolResult.IsSome())
		})
	}
}

// TestCompactedHistoryAbsentMarkerAndBoundary verifies no-marker projection and nil history for a missing boundary.
func TestCompactedHistoryAbsentMarkerAndBoundary(t *testing.T) {
	t.Parallel()
	// Arrange ordinary history and a marker that names no retained entry.
	entries := []session.Entry{newCompactionUserEntry("user", mo.None[string](), "")}
	missingBoundary := append([]session.Entry{entries[0]}, newCompactionMarkerEntry("marker", "user", "", "missing"))

	// Act through the same backward marker scan for both inputs.
	ordinary := compactedHistoryFromEntries(entries)
	missing := compactedHistoryFromEntries(missingBoundary)

	// Assert the empty user message survives and a missing boundary still yields nil.
	require.Equal(t, historyFromEntries(entries), ordinary)
	require.Len(t, ordinary, 1)
	require.Empty(t, ordinary[0].History.User.MustGet().Text(""))
	require.Nil(t, missing)
}
