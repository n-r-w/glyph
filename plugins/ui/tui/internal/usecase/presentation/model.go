// Package presentation owns private TUI projection and interaction state.
package presentation

import (
	"strings"

	"github.com/samber/mo"
)

// TreeMode identifies the focused tree interaction state.
type TreeMode int

const (
	// TreeClosed leaves focus in the main editor.
	TreeClosed TreeMode = iota
	// TreeSelect focuses the tree rows and local search.
	TreeSelect
	// TreeSummary focuses the navigation summary choices.
	TreeSummary
	// TreeCustomFocus edits a required custom summary focus.
	TreeCustomFocus
	// TreeLabel edits the selected entry label.
	TreeLabel
)

// interaction is the single root Bubble Tea presentation and input model.
type interaction struct {
	// state contains the current TUI presentation projection.
	state projection
	// projectionChanged invalidates detached display data when a projection mutation occurs.
	projectionChanged bool
	// input contains editable user request runes.
	input []rune
	// cursor is the insertion position within input.
	cursor int
	// emitting prevents overlapping commands until the current stream send returns.
	emitting bool
	// selectorOpen routes keys away from the editor into the visible selector.
	selectorOpen bool
	// sessionSelector distinguishes resume rows from model rows while reusing navigation state.
	sessionSelector bool
	// resumePending keeps one selected session stable until Host accepts or rejects replacement.
	resumePending bool
	// resumeStatus shows a Host rejection without adding it to the active transcript.
	resumeStatus string
	// selectorRow is the selected model or session row.
	selectorRow int
	// reasoningExpanded controls only local display and never changes Host selection.
	reasoningExpanded bool
	// branchSummariesExpanded controls local branch-summary presentation.
	branchSummariesExpanded bool
	// treePanel contains the current committed tree and local presentation state.
	treePanel mo.Option[treePanel]
	// treeRequest identifies a tree snapshot request that has not returned.
	treeRequest mo.Option[TreePurpose]
	// treeMode identifies the focused tree interaction.
	treeMode TreeMode
	// treeSummaryIndex identifies the selected summary choice.
	treeSummaryIndex int
	// treeInput contains local search, label, or custom-focus text.
	treeInput []rune
	// treeCursor identifies the insertion point in treeInput.
	treeCursor int
	// treeAwaiting identifies a command sent but not yet resolved by a Host frame.
	treeAwaiting CommandKind
	// treeStatus contains the latest safe tree operation result.
	treeStatus string
}

// emissionResultMsg returns command-delivery success or failure to the update loop.
type emissionResultMsg struct {
	// command contains the attempted Host command.
	command Command
	// err contains the command delivery failure.
	err error
}

// newInteraction creates the root model from the initialization event.
func newInteraction(initial event) interaction {
	return interaction{
		state:                   (projection{}).Apply(initial),
		projectionChanged:       true,
		input:                   nil,
		cursor:                  0,
		emitting:                false,
		selectorOpen:            false,
		selectorRow:             0,
		reasoningExpanded:       false,
		branchSummariesExpanded: false,
		treePanel:               mo.None[treePanel](),
		treeRequest:             mo.None[TreePurpose](),
		treeMode:                TreeClosed,
		treeSummaryIndex:        0,
		treeInput:               nil,
		treeCursor:              0,
		treeAwaiting:            CommandUnspecified,
		treeStatus:              "",
		sessionSelector:         false,
		resumePending:           false,
		resumeStatus:            "",
	}
}

// insertText adds pasted or typed Unicode text at the rune cursor.
func (model *interaction) insertText(text string) {
	text = strings.NewReplacer("\r", "", "\n", "").Replace(text)
	if text == "" {
		return
	}
	runes := []rune(text)
	model.input = append(model.input[:model.cursor], append(runes, model.input[model.cursor:]...)...)
	model.cursor += len(runes)
}
