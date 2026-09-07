package presentation

import (
	"slices"

	"github.com/samber/mo"
)

// DisplayBody contains detached projection data needed by terminal rendering.
type DisplayBody struct {
	// Startup contains ordered startup diagnostics.
	Startup []Line
	// Transcript contains completed display blocks.
	Transcript []Line
	// ActiveModel contains streamed model fragments by position.
	ActiveModel map[int]ActiveModelContent
	// ActiveToolCalls contains active tool-call display data by identifier.
	ActiveToolCalls map[string]ToolCallState
	// Models contains configured model display choices.
	Models []ConfiguredModel
	// Sessions contains stored-session display choices.
	Sessions []SessionSummary
	// Availability is the Host admission state shown in the status line.
	Availability mo.Option[Availability]
	// AuthorizationURL contains the latest authorization URL.
	AuthorizationURL mo.Option[string]
	// ModelSelection is the Host-confirmed selected model.
	ModelSelection mo.Option[ModelSelection]
}

// TreeView contains semantic visible rows and immutable selector display data.
type TreeView struct {
	// Rows contains selectable entries in application order without connector geometry.
	Rows []VisibleEntry
	// SelectedID identifies the highlighted visible entry.
	SelectedID mo.Option[string]
	// Filter identifies the active semantic filter.
	Filter TreeFilter
	// Query contains the active search text.
	Query string
}

// Snapshot is coherent immutable view data, not an application state handle.
type Snapshot struct {
	// Body contains detached transcript and active-content data.
	Body DisplayBody
	// Input contains the editor draft.
	Input []rune
	// Cursor is the draft insertion position.
	Cursor int
	// SelectorOpen identifies an open model or session selector.
	SelectorOpen bool
	// SessionSelector selects session rows instead of model rows.
	SessionSelector bool
	// ResumeStatus contains a resume rejection shown beside its retained selector.
	ResumeStatus string
	// SelectorRow identifies the selected model or session row.
	SelectorRow int
	// ReasoningExpanded controls transcript reasoning visibility.
	ReasoningExpanded bool
	// BranchSummariesExpanded controls branch-summary visibility.
	BranchSummariesExpanded bool
	// ReasoningSelectionVisible determines whether a real reasoning alternative exists.
	ReasoningSelectionVisible bool
	// TreePanel contains the current semantic tree display when open.
	TreePanel mo.Option[TreeView]
	// TreeMode identifies the focused tree dialog.
	TreeMode TreeMode
	// TreeSummaryIndex identifies the selected summary choice.
	TreeSummaryIndex int
	// TreeInput contains the dialog draft.
	TreeInput []rune
	// TreeCursor identifies the dialog draft insertion position.
	TreeCursor int
	// TreeStatus contains the latest tree operation information.
	TreeStatus string
}

// publish detaches changed projection data and publishes one coherent interaction snapshot.
func (service *Service) publish() {
	if service.model.projectionChanged {
		// Editor-only transitions reuse this detached body instead of cloning the full transcript per key.
		state := service.model.state.Clone()
		service.model.projectionChanged = false
		service.body = DisplayBody{
			Startup:          state.Startup,
			Transcript:       state.Transcript,
			ActiveModel:      state.ActiveModel,
			ActiveToolCalls:  state.ActiveToolCalls,
			Models:           state.Models,
			Sessions:         state.Sessions,
			Availability:     state.Availability,
			AuthorizationURL: state.AuthorizationURL,
			ModelSelection:   state.ModelSelection,
		}
	}
	tree := mo.None[TreeView]()
	if panel, present := service.model.treePanel.Get(); present {
		tree = mo.Some(
			TreeView{
				Rows:       panel.visibleEntries(),
				SelectedID: panel.SelectedID,
				Filter:     panel.Filter,
				Query:      panel.Query,
			},
		)
	}
	reasoningVisible := true
	if len(service.model.state.Models) > 0 {
		selected := service.model.state.Models[service.model.currentModelIndex()]
		reasoningVisible = len(selected.Reasoning.Choices) > 1
	}
	service.display.Publish(Snapshot{
		Body:                      service.body,
		Input:                     slices.Clone(service.model.input),
		Cursor:                    service.model.cursor,
		SelectorOpen:              service.model.selectorOpen,
		SessionSelector:           service.model.sessionSelector,
		ResumeStatus:              service.model.resumeStatus,
		SelectorRow:               service.model.selectorRow,
		ReasoningExpanded:         service.model.reasoningExpanded,
		BranchSummariesExpanded:   service.model.branchSummariesExpanded,
		ReasoningSelectionVisible: reasoningVisible,
		TreePanel:                 tree,
		TreeMode:                  service.model.treeMode,
		TreeSummaryIndex:          service.model.treeSummaryIndex,
		TreeInput:                 slices.Clone(service.model.treeInput),
		TreeCursor:                service.model.treeCursor,
		TreeStatus:                service.model.treeStatus,
	})
}
