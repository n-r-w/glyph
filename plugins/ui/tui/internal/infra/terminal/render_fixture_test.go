//go:build !integration

package terminal

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// newRenderModel creates output-only view data without application transition behavior.
func newRenderModel() Model {
	return Model{snapshot: presentation.Snapshot{
		Body: presentation.DisplayBody{
			Startup: nil, Transcript: nil, ActiveModel: nil, ActiveToolCalls: nil, Models: nil, Sessions: nil,
			Availability: mo.Some(presentation.AvailabilityIdle), AuthorizationURL: mo.None[string](),
			ModelSelection: mo.None[presentation.ModelSelection](),
		},
		Input: nil, Cursor: 0, SelectorOpen: false, SessionSelector: false, ResumeStatus: "", SelectorRow: 0,
		ReasoningExpanded: false, BranchSummariesExpanded: false, ReasoningSelectionVisible: true,
		TreePanel: mo.None[presentation.TreeView](), TreeMode: presentation.TreeClosed,
		TreeSummaryIndex: 0, TreeInput: nil, TreeCursor: 0, TreeStatus: "",
	}, input: nil, plugin: nil, width: 0, height: 0, failure: nil, notificationEnded: false}
}

// renderTextLine creates one immutable text block for output tests.
func renderTextLine(kind presentation.LineKind, text string) presentation.Line {
	return presentation.Line{
		Kind: kind, Text: mo.Some(text), ToolName: mo.None[string](),
		Status: mo.None[string](), Contents: mo.None[[]presentation.Content](),
	}
}
