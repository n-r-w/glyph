package tui

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/lo"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

const (
	// treeSummaryChoiceCount is the closed number of summary choices.
	treeSummaryChoiceCount = 3
	// treeSummarySummarizeIndex identifies the default-summary choice.
	treeSummarySummarizeIndex = 1
	// treeSummaryCustomIndex identifies the custom-focus choice.
	treeSummaryCustomIndex = 2
)

// updateFocusedTreeKey blocks pending operations and routes open tree interactions.
func (model Model) updateFocusedTreeKey(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if model.treeAwaiting != presentationdomain.CommandUnspecified {
		return model, nil, true
	}
	if model.treeMode == treeInteractionClosed {
		return model, nil, false
	}
	updated, command := model.updateTreeKey(key)
	return updated, command, true
}

// updateTreeKey routes keys to the focused tree interaction.
func (model Model) updateTreeKey(key tea.Key) (tea.Model, tea.Cmd) {
	if model.treeAwaiting != presentationdomain.CommandUnspecified {
		return model, nil
	}
	switch model.treeMode {
	case treeInteractionSelect:
		return model.updateTreeSelectionKey(key)
	case treeInteractionSummary:
		return model.updateTreeSummaryKey(key)
	case treeInteractionCustomFocus, treeInteractionLabel:
		return model.updateTreeInputKey(key)
	case treeInteractionClosed:
		return model, nil
	default:
		return model, nil
	}
}

// updateTreeSelectionKey handles local tree search, filters, folding, labels, and target selection.
func (model Model) updateTreeSelectionKey(key tea.Key) (tea.Model, tea.Cmd) {
	panel, present := model.treePanel.Get()
	if !present {
		return model.closeTree(), nil
	}
	if key.Code == tea.KeyEscape {
		if panel.Query != "" {
			panel.SetQuery("")
			model.treePanel = mo.Some(panel)
			return model, nil
		}
		return model.closeTree(), nil
	}
	if key.Code == tea.KeyBackspace {
		query := []rune(panel.Query)
		if len(query) > 0 {
			panel.SetQuery(string(query[:len(query)-1]))
			model.treePanel = mo.Some(panel)
		}
		return model, nil
	}
	if updated, handled := model.updateTreeSelectionModifier(panel, key); handled {
		return updated, nil
	}
	switch key.Code {
	case tea.KeyUp:
		panel.MoveSelection(-1)
		model.treePanel = mo.Some(panel)
	case tea.KeyDown:
		panel.MoveSelection(1)
		model.treePanel = mo.Some(panel)
	case tea.KeyEnter:
		return model.confirmTreeSelection(panel)
	case tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd, tea.KeyDelete:
		return model, nil
	default:
		if key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta) == 0 && key.Text != "" {
			panel.SetQuery(panel.Query + strings.NewReplacer("\r", "", "\n", "").Replace(key.Text))
			model.treePanel = mo.Some(panel)
		}
	}
	return model, nil
}

// updateTreeSelectionModifier handles filter, label, and fold shortcuts.
func (model Model) updateTreeSelectionModifier(
	panel presentationdomain.TreePanel,
	key tea.Key,
) (Model, bool) {
	if key.Mod == tea.ModCtrl|tea.ModShift && unicode.ToLower(key.Code) == 'o' {
		panel.SetFilter(nextTreeFilter(panel.Filter, true))
		model.treePanel = mo.Some(panel)
		return model, true
	}
	if key.Mod == tea.ModCtrl && key.Code != tea.KeyLeft && key.Code != tea.KeyRight {
		filter, handled := treeFilterShortcut(panel.Filter, unicode.ToLower(key.Code))
		if !handled {
			return model, false
		}
		panel.SetFilter(filter)
		model.treePanel = mo.Some(panel)
		return model, true
	}
	if key.Mod == tea.ModShift && unicode.ToLower(key.Code) == 'l' {
		return model.openTreeLabel(panel), true
	}
	return model.updateTreeFoldModifier(panel, key)
}

// openTreeLabel opens committed-label editing for the selected entry.
func (model Model) openTreeLabel(panel presentationdomain.TreePanel) Model {
	selected, present := selectedTreeEntry(panel)
	if !present {
		return model
	}
	model.treeMode = treeInteractionLabel
	model.treeInput = []rune(selected.Label)
	model.treeCursor = len(model.treeInput)
	return model
}

// updateTreeFoldModifier handles only documented modified arrow keys.
func (model Model) updateTreeFoldModifier(panel presentationdomain.TreePanel, key tea.Key) (Model, bool) {
	if key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 || key.Code != tea.KeyLeft && key.Code != tea.KeyRight {
		return model, false
	}
	selected, present := selectedTreeRow(panel)
	if !present {
		return model, true
	}
	if key.Code == tea.KeyLeft && selected.HasChildren && !selected.Folded ||
		key.Code == tea.KeyRight && selected.Folded {
		panel.ToggleFold()
		model.treePanel = mo.Some(panel)
	}
	return model, true
}

// updateTreeSummaryKey handles the three documented summary choices.
func (model Model) updateTreeSummaryKey(key tea.Key) (tea.Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape:
		model.treeMode = treeInteractionSelect
	case tea.KeyUp:
		model.treeSummaryIndex = max(0, model.treeSummaryIndex-1)
	case tea.KeyDown:
		model.treeSummaryIndex = min(treeSummaryChoiceCount-1, model.treeSummaryIndex+1)
	case tea.KeyEnter:
		if model.treeSummaryIndex == treeSummaryCustomIndex {
			model.treeMode = treeInteractionCustomFocus
			model.treeInput = nil
			model.treeCursor = 0
			return model, nil
		}
		mode := presentationdomain.SummaryModeNoSummary
		if model.treeSummaryIndex == treeSummarySummarizeIndex {
			mode = presentationdomain.SummaryModeSummarize
		}
		return model.emitNavigation(mode, mo.None[string]())
	}
	return model, nil
}

// updateTreeInputKey edits label and custom-focus text without changing the main editor.
func (model Model) updateTreeInputKey(key tea.Key) (tea.Model, tea.Cmd) {
	if key.Code == tea.KeyEscape {
		if model.treeMode == treeInteractionCustomFocus {
			model.treeMode = treeInteractionSummary
		} else {
			model.treeMode = treeInteractionSelect
		}
		model.treeInput = nil
		model.treeCursor = 0
		return model, nil
	}
	switch key.Code {
	case tea.KeyEnter:
		return model.confirmTreeInput()
	case tea.KeyLeft:
		model.treeCursor = max(0, model.treeCursor-1)
	case tea.KeyRight:
		model.treeCursor = min(len(model.treeInput), model.treeCursor+1)
	case tea.KeyHome:
		model.treeCursor = 0
	case tea.KeyEnd:
		model.treeCursor = len(model.treeInput)
	case tea.KeyBackspace:
		if model.treeCursor > 0 {
			model.treeInput = append(model.treeInput[:model.treeCursor-1], model.treeInput[model.treeCursor:]...)
			model.treeCursor--
		}
	case tea.KeyDelete:
		if model.treeCursor < len(model.treeInput) {
			model.treeInput = append(model.treeInput[:model.treeCursor], model.treeInput[model.treeCursor+1:]...)
		}
	default:
		if key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta) == 0 && key.Text != "" {
			text := []rune(strings.NewReplacer("\r", "", "\n", "").Replace(key.Text))
			model.treeInput = slices.Insert(model.treeInput, model.treeCursor, text...)
			model.treeCursor += len(text)
		}
	}
	return model, nil
}

// selectedTreeRow returns the selected visible row.
func selectedTreeRow(panel presentationdomain.TreePanel) (presentationdomain.TreeRow, bool) {
	selectedID, present := panel.SelectedID.Get()
	if !present {
		return presentationdomain.TreeRow{}, false
	}
	return lo.Find(panel.VisibleRows(), func(row presentationdomain.TreeRow) bool { return row.Entry.ID == selectedID })
}

// treeFilterShortcut maps one direct filter shortcut.
func treeFilterShortcut(current presentationdomain.TreeFilter, key rune) (presentationdomain.TreeFilter, bool) {
	switch key {
	case 'd':
		return presentationdomain.TreeFilterDefault, true
	case 't':
		return presentationdomain.TreeFilterNoTools, true
	case 'u':
		return presentationdomain.TreeFilterUserOnly, true
	case 'l':
		return presentationdomain.TreeFilterLabeledOnly, true
	case 'a':
		return presentationdomain.TreeFilterAll, true
	case 'o':
		return nextTreeFilter(current, false), true
	default:
		return current, false
	}
}

// nextTreeFilter cycles the five documented visibility filters.
func nextTreeFilter(filter presentationdomain.TreeFilter, reverse bool) presentationdomain.TreeFilter {
	filters := []presentationdomain.TreeFilter{
		presentationdomain.TreeFilterDefault,
		presentationdomain.TreeFilterNoTools,
		presentationdomain.TreeFilterUserOnly,
		presentationdomain.TreeFilterLabeledOnly,
		presentationdomain.TreeFilterAll,
	}
	index := slices.Index(filters, filter)
	if reverse {
		return filters[(index-1+len(filters))%len(filters)]
	}
	return filters[(index+1)%len(filters)]
}
