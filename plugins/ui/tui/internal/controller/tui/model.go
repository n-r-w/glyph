// Package tui owns the standard terminal presentation and Bubble Tea event loop.
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Apply projects one Host event into presentation state.
type Apply func(presentationdomain.State, presentationdomain.Event) presentationdomain.State

// Emit sends one accepted user command to the Host stream.
type Emit func(presentationdomain.Command) error

// treeInteractionMode identifies the focused tree interaction state.
type treeInteractionMode int

const (
	// treeInteractionClosed leaves focus in the main editor.
	treeInteractionClosed treeInteractionMode = iota
	// treeInteractionSelect focuses the tree rows and local search.
	treeInteractionSelect
	// treeInteractionSummary focuses the navigation summary choices.
	treeInteractionSummary
	// treeInteractionCustomFocus edits a required custom summary focus.
	treeInteractionCustomFocus
	// treeInteractionLabel edits the selected entry label.
	treeInteractionLabel
)

// Model is the single root Bubble Tea presentation and input model.
type Model struct {
	// state contains the current TUI presentation projection.
	state presentationdomain.State
	// input contains editable user request runes.
	input []rune
	// cursor is the insertion position within input.
	cursor int
	// width is the current terminal width.
	width int
	// height is the current terminal height.
	height int
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
	// treePanel contains the current committed tree and local presentation state.
	treePanel mo.Option[presentationdomain.TreePanel]
	// treeRequest identifies a tree snapshot request that has not returned.
	treeRequest mo.Option[presentationdomain.TreePurpose]
	// treeMode identifies the focused tree interaction.
	treeMode treeInteractionMode
	// treeSummaryIndex identifies the selected summary choice.
	treeSummaryIndex int
	// treeInput contains local search, label, or custom-focus text.
	treeInput []rune
	// treeCursor identifies the insertion point in treeInput.
	treeCursor int
	// treeAwaiting identifies a command sent but not yet resolved by a Host frame.
	treeAwaiting presentationdomain.CommandKind
	// treeStatus contains the latest safe tree operation result.
	treeStatus string
	// apply projects one Host event into presentation state.
	apply Apply
	// emit sends one accepted user command to the Host.
	emit Emit
}

var _ tea.Model = (*Model)(nil)

const (
	// inactiveSelectorPrefix marks an unselected selector row.
	inactiveSelectorPrefix = "  "
	// activeSelectorPrefix marks the selected selector row.
	activeSelectorPrefix = "> "
)

const (
	// fixedViewLineCount is the non-selector viewport height.
	fixedViewLineCount = 5
	// selectorFixedLineCount is the selector heading and help height.
	selectorFixedLineCount = 2
	// maxVisibleSelectorRows limits selector body height.
	maxVisibleSelectorRows = 5
	// selectorCenterDivisor centers the selected row.
	selectorCenterDivisor = 2
)

// emissionResultMsg returns command-delivery success or failure to the update loop.
type emissionResultMsg struct {
	// command contains the attempted Host command.
	command presentationdomain.Command
	// err contains the command delivery failure.
	err error
}

// NewModel creates the root model from the initialization event.
func NewModel(initial presentationdomain.Event, apply Apply, emit Emit) Model {
	return Model{
		state:             apply(presentationdomain.State{}, initial),
		input:             nil,
		cursor:            0,
		width:             0,
		height:            0,
		emitting:          false,
		selectorOpen:      false,
		selectorRow:       0,
		reasoningExpanded: false,
		treePanel:         mo.None[presentationdomain.TreePanel](),
		treeRequest:       mo.None[presentationdomain.TreePurpose](),
		treeMode:          treeInteractionClosed,
		treeSummaryIndex:  0,
		treeInput:         nil,
		treeCursor:        0,
		treeAwaiting:      presentationdomain.CommandUnspecified,
		treeStatus:        "",
		apply:             apply,
		emit:              emit,
		sessionSelector:   false,
		resumePending:     false,
		resumeStatus:      "",
	}
}

// Init starts no background presentation work.
func (Model) Init() tea.Cmd {
	return nil
}

// insertText adds pasted or typed Unicode text at the rune cursor.
func (model *Model) insertText(text string) {
	text = strings.NewReplacer("\r", "", "\n", "").Replace(text)
	if text == "" {
		return
	}
	runes := []rune(text)
	model.input = append(model.input[:model.cursor], append(runes, model.input[model.cursor:]...)...)
	model.cursor += len(runes)
}
