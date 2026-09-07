package terminal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/samber/lo"
	"github.com/samber/mo"

	presentation "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

const (
	// unknownTreeText identifies an unspecified tree value.
	unknownTreeText = "unknown"
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
	// treeAncestorContinuation continues one visible ancestor branch.
	treeAncestorContinuation = "│  "
	// treeAncestorSpacing reserves one closed ancestor branch level.
	treeAncestorSpacing = "   "
	// treeBranchMiddleConnector connects a node before a later visible sibling.
	treeBranchMiddleConnector = "├─ "
	// treeBranchLastConnector connects the last visible sibling.
	treeBranchLastConnector = "└─ "
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
	// treeRowEllipsis marks a tree row truncated to terminal width.
	treeRowEllipsis = "…"
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
	if model.snapshot.TreeMode == presentation.TreeClosed {
		return nil
	}
	panel, present := model.snapshot.TreePanel.Get()
	if !present {
		return []string{treeWaitingText}
	}
	switch model.snapshot.TreeMode {
	case presentation.TreeSummary:
		choices := []string{treeSummaryNoSummary, treeSummarySummarize, treeSummaryCustom}
		lines := lo.Map(choices, func(choice string, index int) string {
			prefix := inactiveSelectorPrefix
			if index == model.snapshot.TreeSummaryIndex {
				prefix = activeSelectorPrefix
			}
			return prefix + choice
		})
		lines = append([]string{treeSummaryTitle}, lines...)
		return append(lines, treeSummaryHelpText)
	case presentation.TreeCustomFocus:
		return []string{
			treeCustomFocusTitle,
			treeInputLine(model.snapshot.TreeInput, model.snapshot.TreeCursor),
			treeCustomFocusHelpText,
		}
	case presentation.TreeLabel:
		return []string{
			treeLabelTitle,
			treeInputLine(model.snapshot.TreeInput, model.snapshot.TreeCursor),
			treeLabelHelpText,
		}
	case presentation.TreeSelect:
		rows := treeRows(panel.Rows)
		capacity := min(maxVisibleSelectorRows, len(rows))
		selectedIndex := slices.IndexFunc(rows, func(row treeRow) bool {
			return panel.SelectedID == mo.Some(row.Entry.ID)
		})
		start := max(0, selectedIndex-capacity/selectorCenterDivisor)
		start = min(start, max(0, len(rows)-capacity))
		visibleRows := rows[start : start+capacity]
		lines := lo.Map(visibleRows, func(row treeRow, _ int) string {
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
		if model.snapshot.TreeStatus != "" {
			status += statusSeparator + model.snapshot.TreeStatus
		}
		lines = append(lines, status)
		return append(lines, treeSelectorHelpText)
	case presentation.TreeClosed:
		return nil
	default:
		return nil
	}
}

// renderTreeRow renders structure, active path, leaf, kind, label, and public text.
func (model Model) renderTreeRow(row treeRow, selected bool) string {
	prefix := inactiveSelectorPrefix
	if selected {
		prefix = activeSelectorPrefix
	}
	ancestorSegments := lo.Map(row.AncestorContinues, func(continues bool, _ int) string {
		if continues {
			return treeAncestorContinuation
		}
		return treeAncestorSpacing
	})
	branch := strings.Join(ancestorSegments, "")
	if row.Depth > 0 {
		connector := treeBranchLastConnector
		if row.HasFollowingSibling {
			connector = treeBranchMiddleConnector
		}
		branch += connector
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
		label = treeLabelPrefix + treeInlineText(row.Entry.Label) + treeLabelSuffix
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
		treeInlineText(row.Entry.Text),
	)
	return ansi.Truncate(text, max(1, model.width), treeRowEllipsis)
}

// treeInlineText normalizes dynamic entry content without changing topology spacing.
func treeInlineText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// treeInputLine renders one local dialog editor with its exact rune cursor.
func treeInputLine(input []rune, cursor int) string {
	return treeInputPrefix + string(input[:cursor]) + treeInputCursor + string(input[cursor:])
}

// treeFilterText returns one stable local filter label.
func treeFilterText(filter presentation.TreeFilter) string {
	switch filter {
	case presentation.TreeFilterDefault:
		return treeFilterDefaultText
	case presentation.TreeFilterNoTools:
		return treeFilterNoToolsText
	case presentation.TreeFilterUserOnly:
		return treeFilterUserOnlyText
	case presentation.TreeFilterLabeledOnly:
		return treeFilterLabeledOnlyText
	case presentation.TreeFilterAll:
		return treeFilterAllText
	default:
		return unknownTreeText
	}
}

// treeEntryKindText returns one stable visible entry kind label.
func treeEntryKindText(kind presentation.TreeEntryKind) string {
	switch kind {
	case presentation.TreeEntryUser:
		return treeEntryUserText
	case presentation.TreeEntryModel:
		return treeEntryModelText
	case presentation.TreeEntryToolResult:
		return treeEntryToolResultText
	case presentation.TreeEntryExtension, presentation.TreeEntryExtensionMessage:
		return treeEntryExtensionText
	case presentation.TreeEntryBranchSummary:
		return treeEntryBranchSummaryText
	case presentation.TreeEntryUnspecified:
		return unknownTreeText
	default:
		return unknownTreeText
	}
}
