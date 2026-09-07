package presentation

import (
	"slices"
	"strings"
	"unicode"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/lo"
	"github.com/samber/mo"
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
func (model interaction) updateFocusedTreeKey(key inputcontroller.Key) (interaction, *commandIntent, bool) {
	if model.treeAwaiting != CommandUnspecified {
		return model, nil, true
	}
	if model.treeMode == TreeClosed {
		return model, nil, false
	}
	updated, command := model.updateTreeKey(key)
	return updated, command, true
}

// updateTreeKey routes keys to the focused tree interaction.
func (model interaction) updateTreeKey(key inputcontroller.Key) (interaction, *commandIntent) {
	if model.treeAwaiting != CommandUnspecified {
		return model, nil
	}
	switch model.treeMode {
	case TreeSelect:
		return model.updateTreeSelectionKey(key)
	case TreeSummary:
		return model.updateTreeSummaryKey(key)
	case TreeCustomFocus, TreeLabel:
		return model.updateTreeInputKey(key)
	case TreeClosed:
		return model, nil
	default:
		return model, nil
	}
}

// updateTreeSelectionKey handles local tree search, filters, folding, labels, and target selection.
func (model interaction) updateTreeSelectionKey(key inputcontroller.Key) (interaction, *commandIntent) {
	panel, present := model.treePanel.Get()
	if !present {
		return model.closeTree(), nil
	}
	if key.Code == inputcontroller.KeyEscape {
		if panel.Query != "" {
			panel.SetQuery("")
			model.treePanel = mo.Some(panel)
			return model, nil
		}
		return model.closeTree(), nil
	}
	if key.Code == inputcontroller.KeyBackspace {
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
	case inputcontroller.KeyUp:
		panel.MoveSelection(-1)
		model.treePanel = mo.Some(panel)
	case inputcontroller.KeyDown:
		panel.MoveSelection(1)
		model.treePanel = mo.Some(panel)
	case inputcontroller.KeyEnter:
		return model.confirmTreeSelection(panel)
	case inputcontroller.KeyLeft,
		inputcontroller.KeyRight,
		inputcontroller.KeyHome,
		inputcontroller.KeyEnd,
		inputcontroller.KeyDelete:
		return model, nil
	default:
		if key.Mod&(inputcontroller.ModCtrl|inputcontroller.ModAlt|inputcontroller.ModMeta) == 0 && key.Text != "" {
			panel.SetQuery(panel.Query + strings.NewReplacer("\r", "", "\n", "").Replace(key.Text))
			model.treePanel = mo.Some(panel)
		}
	}
	return model, nil
}

// updateTreeSelectionModifier handles filter, label, and fold shortcuts.
func (model interaction) updateTreeSelectionModifier(
	panel treePanel,
	key inputcontroller.Key,
) (interaction, bool) {
	if key.Mod == inputcontroller.ModCtrl|inputcontroller.ModShift && unicode.ToLower(key.Code) == 'o' {
		panel.SetFilter(nextTreeFilter(panel.Filter, true))
		model.treePanel = mo.Some(panel)
		return model, true
	}
	if key.Mod == inputcontroller.ModCtrl && key.Code != inputcontroller.KeyLeft &&
		key.Code != inputcontroller.KeyRight {
		filter, handled := treeFilterShortcut(panel.Filter, unicode.ToLower(key.Code))
		if !handled {
			return model, false
		}
		panel.SetFilter(filter)
		model.treePanel = mo.Some(panel)
		return model, true
	}
	if key.Mod == inputcontroller.ModShift && unicode.ToLower(key.Code) == 'l' {
		return model.openTreeLabel(panel), true
	}
	return model.updateTreeFoldModifier(panel, key)
}

// openTreeLabel opens committed-label editing for the selected entry.
func (model interaction) openTreeLabel(panel treePanel) interaction {
	selected, present := selectedTreeEntry(panel)
	if !present {
		return model
	}
	model.treeMode = TreeLabel
	model.treeInput = []rune(selected.Label)
	model.treeCursor = len(model.treeInput)
	return model
}

// updateTreeFoldModifier handles only documented modified arrow keys.
func (model interaction) updateTreeFoldModifier(panel treePanel, key inputcontroller.Key) (interaction, bool) {
	if key.Mod&(inputcontroller.ModCtrl|inputcontroller.ModAlt) == 0 ||
		key.Code != inputcontroller.KeyLeft && key.Code != inputcontroller.KeyRight {
		return model, false
	}
	selected, present := selectedVisibleEntry(panel)
	if !present {
		return model, true
	}
	if key.Code == inputcontroller.KeyLeft && selected.HasChildren && !selected.Folded ||
		key.Code == inputcontroller.KeyRight && selected.Folded {
		panel.ToggleFold()
		model.treePanel = mo.Some(panel)
	}
	return model, true
}

// updateTreeSummaryKey handles the three documented summary choices.
func (model interaction) updateTreeSummaryKey(key inputcontroller.Key) (interaction, *commandIntent) {
	switch key.Code {
	case inputcontroller.KeyEscape:
		model.treeMode = TreeSelect
	case inputcontroller.KeyUp:
		model.treeSummaryIndex = max(0, model.treeSummaryIndex-1)
	case inputcontroller.KeyDown:
		model.treeSummaryIndex = min(treeSummaryChoiceCount-1, model.treeSummaryIndex+1)
	case inputcontroller.KeyEnter:
		if model.treeSummaryIndex == treeSummaryCustomIndex {
			model.treeMode = TreeCustomFocus
			model.treeInput = nil
			model.treeCursor = 0
			return model, nil
		}
		mode := SummaryModeNoSummary
		if model.treeSummaryIndex == treeSummarySummarizeIndex {
			mode = SummaryModeSummarize
		}
		return model.emitNavigation(mode, mo.None[string]())
	}
	return model, nil
}

// updateTreeInputKey edits label and custom-focus text without changing the main editor.
func (model interaction) updateTreeInputKey(key inputcontroller.Key) (interaction, *commandIntent) {
	if key.Code == inputcontroller.KeyEscape {
		if model.treeMode == TreeCustomFocus {
			model.treeMode = TreeSummary
		} else {
			model.treeMode = TreeSelect
		}
		model.treeInput = nil
		model.treeCursor = 0
		return model, nil
	}
	switch key.Code {
	case inputcontroller.KeyEnter:
		return model.confirmTreeInput()
	case inputcontroller.KeyLeft:
		model.treeCursor = max(0, model.treeCursor-1)
	case inputcontroller.KeyRight:
		model.treeCursor = min(len(model.treeInput), model.treeCursor+1)
	case inputcontroller.KeyHome:
		model.treeCursor = 0
	case inputcontroller.KeyEnd:
		model.treeCursor = len(model.treeInput)
	case inputcontroller.KeyBackspace:
		if model.treeCursor > 0 {
			model.treeInput = append(model.treeInput[:model.treeCursor-1], model.treeInput[model.treeCursor:]...)
			model.treeCursor--
		}
	case inputcontroller.KeyDelete:
		if model.treeCursor < len(model.treeInput) {
			model.treeInput = append(model.treeInput[:model.treeCursor], model.treeInput[model.treeCursor+1:]...)
		}
	default:
		if key.Mod&(inputcontroller.ModCtrl|inputcontroller.ModAlt|inputcontroller.ModMeta) == 0 && key.Text != "" {
			text := []rune(strings.NewReplacer("\r", "", "\n", "").Replace(key.Text))
			model.treeInput = slices.Insert(model.treeInput, model.treeCursor, text...)
			model.treeCursor += len(text)
		}
	}
	return model, nil
}

// selectedVisibleEntry returns the selected visible row.
func selectedVisibleEntry(panel treePanel) (VisibleEntry, bool) {
	selectedID, present := panel.SelectedID.Get()
	if !present {
		return VisibleEntry{}, false
	}
	return lo.Find(panel.visibleEntries(), func(row VisibleEntry) bool { return row.Entry.ID == selectedID })
}

// treeFilterShortcut maps one direct filter shortcut.
func treeFilterShortcut(current TreeFilter, key rune) (TreeFilter, bool) {
	switch key {
	case 'd':
		return TreeFilterDefault, true
	case 't':
		return TreeFilterNoTools, true
	case 'u':
		return TreeFilterUserOnly, true
	case 'l':
		return TreeFilterLabeledOnly, true
	case 'a':
		return TreeFilterAll, true
	case 'o':
		return nextTreeFilter(current, false), true
	default:
		return current, false
	}
}

// nextTreeFilter cycles the five documented visibility filters.
func nextTreeFilter(filter TreeFilter, reverse bool) TreeFilter {
	filters := []TreeFilter{
		TreeFilterDefault,
		TreeFilterNoTools,
		TreeFilterUserOnly,
		TreeFilterLabeledOnly,
		TreeFilterAll,
	}
	index := slices.Index(filters, filter)
	if reverse {
		return filters[(index-1+len(filters))%len(filters)]
	}
	return filters[(index+1)%len(filters)]
}
