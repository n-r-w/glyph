//go:build !integration

package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestLinearTreeTopologyOmitsAncestorContinuations preserves connector geometry for a visible chain.
func TestLinearTreeTopologyOmitsAncestorContinuations(t *testing.T) {
	t.Parallel()
	// Arrange application-selected entries with one visible parent each.
	entries := []presentation.VisibleEntry{
		visibleEntry("root", mo.None[string](), "root"),
		visibleEntry("middle", mo.Some("root"), "middle"),
		visibleEntry("leaf", mo.Some("middle"), "leaf"),
	}
	// Act by constructing terminal geometry without choosing visibility.
	rows := treeRows(entries)
	// Assert the chain has depth but no sibling continuation.
	require.Equal(t, []int{0, 1, 2}, lo.Map(rows, func(row treeRow, _ int) int { return row.Depth }))
	require.Empty(t, rows[0].AncestorContinues)
	require.Empty(t, rows[1].AncestorContinues)
	require.Equal(t, []bool{false}, rows[2].AncestorContinues)
	for _, row := range rows {
		require.False(t, row.HasFollowingSibling)
	}
}

// TestBranchedTreeTopologyMarksOnlyRequiredContinuations preserves visible sibling connectors.
func TestBranchedTreeTopologyMarksOnlyRequiredContinuations(t *testing.T) {
	t.Parallel()
	// Arrange a visible branch with a later sibling of its ancestor.
	entries := []presentation.VisibleEntry{
		visibleEntry("root", mo.None[string](), "root"),
		visibleEntry("branch", mo.Some("root"), "branch"),
		visibleEntry("leaf", mo.Some("branch"), "leaf"),
		visibleEntry("sibling", mo.Some("root"), "sibling"),
	}
	// Act by computing output geometry.
	rows := treeRows(entries)
	// Assert only the continued ancestor level receives a vertical connector.
	require.True(t, rows[1].HasFollowingSibling)
	require.Equal(t, []bool{true}, rows[2].AncestorContinues)
	require.False(t, rows[2].HasFollowingSibling)
	require.Empty(t, rows[3].AncestorContinues)
	require.False(t, rows[3].HasFollowingSibling)
}

// TestTreeRenderingKeepsMultilineEntryTextOnOneRow preserves topology while normalizing displayed content.
func TestTreeRenderingKeepsMultilineEntryTextOnOneRow(t *testing.T) {
	t.Parallel()
	// Arrange a visible tree entry with multiline public text and a label.
	entry := visibleEntry("entry", mo.None[string](), "first\n\nsecond\tpart")
	entry.Entry.Label = "line\nlabel"
	model := newRenderModel()
	model.width = 200
	row := treeRows([]presentation.VisibleEntry{entry})[0]
	// Act by rendering one selected row.
	text := model.renderTreeRow(row, true)
	// Assert content stays inline without changing selection or row geometry.
	require.NotContains(t, text, "\n")
	require.Contains(t, text, "first second part")
	require.Contains(t, text, "[line label]")
	require.True(t, strings.HasPrefix(text, activeSelectorPrefix))
}

// TestTreeSelectionMovementPreservesVisibleTopology changes only the selection prefix for identical semantic rows.
func TestTreeSelectionMovementPreservesVisibleTopology(t *testing.T) {
	t.Parallel()
	// Arrange immutable semantic rows with a branch and active leaf.
	entries := []presentation.VisibleEntry{
		visibleEntry("root", mo.None[string](), "root"),
		visibleEntry("left", mo.Some("root"), "left"),
		visibleEntry("right", mo.Some("root"), "right"),
	}
	entries[2].ActiveLeaf = true
	model := newRenderModel()
	model.width = 200
	// Act by rendering each row with both selection states.
	for _, row := range treeRows(entries) {
		selected := model.renderTreeRow(row, true)
		unselected := model.renderTreeRow(row, false)
		// Assert selection cannot alter indentation, connectors, or content.
		require.Equal(
			t,
			strings.TrimPrefix(selected, activeSelectorPrefix),
			strings.TrimPrefix(unselected, inactiveSelectorPrefix),
		)
	}
}

// visibleEntry constructs semantic input without terminal geometry.
func visibleEntry(id string, parent mo.Option[string], text string) presentation.VisibleEntry {
	return presentation.VisibleEntry{
		Entry: presentation.TreeEntry{
			ID: id, ParentID: parent, CreatedAt: time.Unix(1, 0), Label: "",
			Kind: presentation.TreeEntryUser, ExtensionMessage: mo.None[presentation.ExtensionMessage](), Text: text,
		},
		ParentID: parent, ActivePath: false, ActiveLeaf: false, Context: false, Folded: false, HasChildren: false,
	}
}
