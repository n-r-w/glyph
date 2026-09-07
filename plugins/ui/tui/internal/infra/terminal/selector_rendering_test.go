//go:build !integration

package terminal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelSelectorFitsTerminal keeps both ends of an application-selected model list visible within the viewport.
func TestModelSelectorFitsTerminal(t *testing.T) {
	t.Parallel()
	// Arrange eight immutable model choices and a five-line selector budget.
	model := newRenderModel()
	model.height = 10
	model.snapshot.SelectorOpen = true
	model.snapshot.Input, model.snapshot.Cursor = []rune("draft"), 5
	for index := range 8 {
		model.snapshot.Body.Models = append(model.snapshot.Body.Models, presentation.ConfiguredModel{
			ProviderID: "provider",
			ModelID:    fmt.Sprintf("model-%d", index),
			Reasoning:  presentation.ReasoningCapabilities{},
		})
	}
	// Act by rendering the two application-selected ends of the list.
	first := model.View().Content
	model.snapshot.SelectorRow = 7
	last := model.View().Content
	// Assert selected rows fit without hiding the editor or mutating the list.
	require.Contains(t, first, "> provider / model-0")
	require.NotContains(t, first, "provider / model-7")
	require.Contains(t, last, "> provider / model-7")
	require.LessOrEqual(t, len(strings.Split(first, "\n")), model.height)
	require.LessOrEqual(t, len(strings.Split(last, "\n")), model.height)
	require.Contains(t, last, "Request: draft|")
	require.Len(t, model.snapshot.Body.Models, 8)
}

// TestModelResumeRejectionReservesHeightAndFitsTerminalWidth preserves selector error geometry.
func TestModelResumeRejectionReservesHeightAndFitsTerminalWidth(t *testing.T) {
	t.Parallel()
	// Arrange a retained resume rejection in a narrow terminal.
	model := newRenderModel()
	model.width, model.height = 24, fixedViewLineCount+selectorFixedLineCount+1+2
	model.snapshot.SelectorOpen, model.snapshot.SessionSelector = true, true
	model.snapshot.ResumeStatus = "Session replacement is unavailable because the storage path cannot be opened."
	for index := range 5 {
		model.snapshot.Body.Sessions = append(model.snapshot.Body.Sessions, presentation.SessionSummary{
			Info: presentation.SessionInfo{
				ID: fmt.Sprintf("stored-%d", index), Name: "", NamePresent: false,
				WorkingDirectory: "/project", StoragePath: "", StoragePresent: false,
				CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
			},
			FirstUserText: "", TextPresent: false, TotalMessages: 1,
		})
	}
	// Act by rendering the retained selector snapshot.
	lines := model.visibleSelectorLines()
	// Assert the error reserves height and uses terminal-cell truncation.
	require.Len(t, lines, 5)
	require.Contains(t, lines[len(lines)-2], sessionStatusLabel)
	require.LessOrEqual(t, ansi.StringWidth(lines[len(lines)-2]), model.width)
}

// TestTreeViewRendersStructureActiveStateKindsLabelsAndSummary preserves tree and summary output from semantic rows.
func TestTreeViewRendersStructureActiveStateKindsLabelsAndSummary(t *testing.T) {
	t.Parallel()
	// Arrange one active labeled branch-summary entry with folded children.
	entry := visibleEntry("summary", mo.None[string](), "branch text")
	entry.Entry.Kind, entry.Entry.Label = presentation.TreeEntryBranchSummary, "important"
	entry.ActivePath, entry.ActiveLeaf, entry.HasChildren, entry.Folded = true, true, true, true
	model := newRenderModel()
	model.width = 200
	model.snapshot.TreeMode = presentation.TreeSelect
	model.snapshot.TreePanel = mo.Some(presentation.TreeView{
		Rows: []presentation.VisibleEntry{entry}, SelectedID: mo.Some("summary"),
		Filter: presentation.TreeFilterAll, Query: "branch",
	})
	// Act by rendering tree selection and then its summary-choice snapshot.
	view := model.View().Content
	model.snapshot.TreeMode = presentation.TreeSummary
	summary := model.View().Content
	// Assert semantic flags, labels, search, and dialog choice remain visible.
	require.Contains(t, view, "> + * leaf [important] branch summary: branch text")
	require.Contains(t, view, "Filter: all")
	require.Contains(t, view, "Search: branch")
	require.Contains(t, summary, "Branch summary:")
	require.Contains(t, summary, "> No summary")
}
