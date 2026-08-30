package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

const (
	// unknownTreeText identifies an unspecified tree value.
	unknownTreeText = "unknown"
	// treeOperationFailedText reports an unclassified tree failure.
	treeOperationFailedText = "Session tree operation failed"
)

const (
	// treeWaitingText reports that the Host tree response is pending.
	treeWaitingText = "Session tree: waiting for Host"
	// treeSelectorTitle labels tree entry selection.
	treeSelectorTitle = "Session tree:"
	// treeNoEntriesText reports an empty tree projection.
	treeNoEntriesText = "  No entries found"
	// treeFilterLabel prefixes the active filter.
	treeFilterLabel = "Filter: "
	// treeSearchLabel prefixes the active search query.
	treeSearchLabel = "Search: "
	// treeSelectorHelpText lists tree selection controls.
	treeSelectorHelpText = "Tree: Up/Down | Enter select | Ctrl/Alt+Left/Right fold | " +
		"Shift+L label | Ctrl+O filter | Escape cancel"
)

const (
	// treeSummaryTitle labels summary-mode selection.
	treeSummaryTitle = "Branch summary:"
	// treeSummaryHelpText lists summary selector controls.
	treeSummaryHelpText = "Summary: Up/Down navigate | Enter confirm | Escape back"
	// treeCustomFocusTitle labels custom-focus editing.
	treeCustomFocusTitle = "Custom summary focus:"
	// treeCustomFocusHelpText lists custom-focus controls.
	treeCustomFocusHelpText = "Enter confirm | Escape back"
	// treeLabelTitle labels entry-label editing.
	treeLabelTitle = "Entry label:"
	// treeLabelHelpText lists label editor controls.
	treeLabelHelpText = "Enter save | Escape cancel"
)

const (
	// treeBranchIndent continues one ancestor branch.
	treeBranchIndent = "│  "
	// treeBranchConnector connects one child row.
	treeBranchConnector = "└─ "
	// treeFoldedMarker identifies a folded branch.
	treeFoldedMarker = "+ "
	// treeUnfoldedMarker identifies an unfolded branch.
	treeUnfoldedMarker = "- "
	// treeActivePathMarker identifies an active-path entry.
	treeActivePathMarker = "* "
	// treeActiveLeafMarker identifies the active leaf.
	treeActiveLeafMarker = "* leaf "
	// treeContextMarker identifies an ancestor retained for search context.
	treeContextMarker = "context "
	// treeLabelPrefix starts a committed entry label.
	treeLabelPrefix = "["
	// treeLabelSuffix ends a committed entry label.
	treeLabelSuffix = "] "
	// treeRowFormat renders one complete tree row.
	treeRowFormat = "%s%s%s%s%s%s: %s"
	// treeInputPrefix identifies a focused tree dialog editor.
	treeInputPrefix = "> "
	// treeInputCursor marks the local editor cursor.
	treeInputCursor = "|"
)

const (
	// treeFilterDefaultText labels the default filter.
	treeFilterDefaultText = "default"
	// treeFilterNoToolsText labels the filter without tools.
	treeFilterNoToolsText = "no-tools"
	// treeFilterUserOnlyText labels the user-only filter.
	treeFilterUserOnlyText = "user-only"
	// treeFilterLabeledOnlyText labels the labeled-only filter.
	treeFilterLabeledOnlyText = "labeled-only"
	// treeFilterAllText labels the unfiltered tree.
	treeFilterAllText = "all"
)

const (
	// treeEntryUserText labels a user entry.
	treeEntryUserText = "user"
	// treeEntryModelText labels a model entry.
	treeEntryModelText = "assistant"
	// treeEntryToolResultText labels a tool-result entry.
	treeEntryToolResultText = "tool result"
	// treeEntryExtensionText labels an opaque extension entry.
	treeEntryExtensionText = "extension"
	// treeEntryBranchSummaryText labels a branch-summary entry.
	treeEntryBranchSummaryText = "branch summary"
	// treeIssueSeparator joins safe operation issue messages.
	treeIssueSeparator = "; "
)

const (
	// treeSummaryNoSummary labels navigation without a branch summary.
	treeSummaryNoSummary = "No summary"
	// treeSummarySummarize labels navigation with the default summary prompt.
	treeSummarySummarize = "Summarize"
	// treeSummaryCustom labels navigation with a custom summary focus.
	treeSummaryCustom = "Summarize with custom prompt"
)

// treeSelectorLines renders the focused tree or tree dialog.
func (model Model) treeSelectorLines() []string {
	if model.treeMode == treeInteractionClosed {
		return nil
	}
	panel, present := model.treePanel.Get()
	if !present {
		return []string{treeWaitingText}
	}
	switch model.treeMode {
	case treeInteractionSummary:
		choices := []string{treeSummaryNoSummary, treeSummarySummarize, treeSummaryCustom}
		lines := lo.Map(choices, func(choice string, index int) string {
			prefix := inactiveSelectorPrefix
			if index == model.treeSummaryIndex {
				prefix = activeSelectorPrefix
			}
			return prefix + choice
		})
		lines = append([]string{treeSummaryTitle}, lines...)
		return append(lines, treeSummaryHelpText)
	case treeInteractionCustomFocus:
		return []string{
			treeCustomFocusTitle,
			treeInputLine(model.treeInput, model.treeCursor),
			treeCustomFocusHelpText,
		}
	case treeInteractionLabel:
		return []string{treeLabelTitle, treeInputLine(model.treeInput, model.treeCursor), treeLabelHelpText}
	case treeInteractionSelect:
		rows := panel.VisibleRows()
		capacity := min(maxVisibleSelectorRows, len(rows))
		selectedIndex := slices.IndexFunc(rows, func(row presentationdomain.TreeRow) bool {
			return panel.SelectedID == mo.Some(row.Entry.ID)
		})
		start := max(0, selectedIndex-capacity/selectorCenterDivisor)
		start = min(start, max(0, len(rows)-capacity))
		visibleRows := rows[start : start+capacity]
		lines := lo.Map(visibleRows, func(row presentationdomain.TreeRow, _ int) string {
			return model.renderTreeRow(row, panel.SelectedID == mo.Some(row.Entry.ID))
		})
		lines = append([]string{treeSelectorTitle}, lines...)
		if len(rows) == 0 {
			lines = append(lines, treeNoEntriesText)
		}
		status := treeFilterLabel + treeFilterText(panel.Filter)
		if panel.Query != "" {
			status += statusSeparator + treeSearchLabel + panel.Query
		}
		if model.treeStatus != "" {
			status += statusSeparator + model.treeStatus
		}
		lines = append(lines, status)
		return append(lines, treeSelectorHelpText)
	case treeInteractionClosed:
		return nil
	default:
		return nil
	}
}

// renderTreeRow renders structure, active path, leaf, kind, label, and public text.
func (model Model) renderTreeRow(row presentationdomain.TreeRow, selected bool) string {
	prefix := inactiveSelectorPrefix
	if selected {
		prefix = activeSelectorPrefix
	}
	branch := strings.Repeat(treeBranchIndent, max(0, row.Depth-1))
	if row.Depth > 0 {
		branch += treeBranchConnector
	}
	fold := ""
	if row.HasChildren {
		if row.Folded {
			fold = treeFoldedMarker
		} else {
			fold = treeUnfoldedMarker
		}
	}
	active := ""
	if row.ActivePath {
		active = treeActivePathMarker
	}
	if row.ActiveLeaf {
		active = treeActiveLeafMarker
	}
	label := ""
	if row.Entry.Label != "" {
		label = treeLabelPrefix + row.Entry.Label + treeLabelSuffix
	}
	context := ""
	if row.Context {
		context = treeContextMarker
	}
	text := fmt.Sprintf(
		treeRowFormat,
		prefix,
		branch,
		fold,
		active,
		context,
		label+treeEntryKindText(row.Entry.Kind),
		row.Entry.Text,
	)
	return ellipsize(text, max(1, model.width))
}

// treeInputLine renders one local dialog editor with its exact rune cursor.
func treeInputLine(input []rune, cursor int) string {
	return treeInputPrefix + string(input[:cursor]) + treeInputCursor + string(input[cursor:])
}

// treeFilterText returns one stable local filter label.
func treeFilterText(filter presentationdomain.TreeFilter) string {
	switch filter {
	case presentationdomain.TreeFilterDefault:
		return treeFilterDefaultText
	case presentationdomain.TreeFilterNoTools:
		return treeFilterNoToolsText
	case presentationdomain.TreeFilterUserOnly:
		return treeFilterUserOnlyText
	case presentationdomain.TreeFilterLabeledOnly:
		return treeFilterLabeledOnlyText
	case presentationdomain.TreeFilterAll:
		return treeFilterAllText
	default:
		return unknownTreeText
	}
}

// treeEntryKindText returns one stable visible entry kind label.
func treeEntryKindText(kind presentationdomain.TreeEntryKind) string {
	switch kind {
	case presentationdomain.TreeEntryUser:
		return treeEntryUserText
	case presentationdomain.TreeEntryModel:
		return treeEntryModelText
	case presentationdomain.TreeEntryToolResult:
		return treeEntryToolResultText
	case presentationdomain.TreeEntryExtension:
		return treeEntryExtensionText
	case presentationdomain.TreeEntryBranchSummary:
		return treeEntryBranchSummaryText
	case presentationdomain.TreeEntryUnspecified:
		return unknownTreeText
	default:
		return unknownTreeText
	}
}

// formatOperationIssues joins safe ordered Host issue messages for local display.
func formatOperationIssues(issues []presentationdomain.OperationIssue) string {
	return strings.Join(lo.Map(issues, func(issue presentationdomain.OperationIssue, _ int) string {
		return issue.Message
	}), treeIssueSeparator)
}
