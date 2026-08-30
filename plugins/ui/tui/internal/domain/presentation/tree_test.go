package presentation

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/suite"
)

// TreePanelSuite verifies client-local tree projection behavior.
type TreePanelSuite struct {
	suite.Suite
}

// TestTreePanelSuite runs tree presentation scenarios.
func TestTreePanelSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(TreePanelSuite))
}

// TestVisibleRowsPreserveStructureAndActiveBranch verifies tree metadata projection.
func (suite *TreePanelSuite) TestVisibleRowsPreserveStructureAndActiveBranch() {
	// Arrange a branched tree with one opaque extension entry and one active summary leaf.
	panel := NewTreePanel(testSessionTree(), TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)

	// Act by projecting the visible rows.
	rows := panel.VisibleRows()
	rowIDs := lo.Map(rows, func(row TreeRow, _ int) string { return row.Entry.ID })

	// Assert structure, labels, active path, and active leaf metadata.
	suite.Require().Equal([]string{"root", "model", "extension", "summary", "alternate"}, rowIDs)
	suite.Require().Equal([]int{0, 1, 2, 2, 2}, lo.Map(rows, func(row TreeRow, _ int) int { return row.Depth }))
	suite.Require().True(rows[0].ActivePath)
	suite.Require().True(rows[1].ActivePath)
	suite.Require().True(rows[3].ActivePath)
	suite.Require().True(rows[3].ActiveLeaf)
	suite.Require().Equal("checkpoint", rows[1].Entry.Label)
	suite.Require().Equal(TreeEntryBranchSummary, rows[3].Entry.Kind)
}

// TestLinearTreeTopologyOmitsAncestorContinuations verifies a chain has no vertical continuation levels.
func (suite *TreePanelSuite) TestLinearTreeTopologyOmitsAncestorContinuations() {
	// Arrange a linear three-entry branch.
	panel := NewTreePanel(SessionTree{
		Entries: []TreeEntry{
			testTreeEntry("root", mo.None[string](), "root", TreeEntryUser, ""),
			testTreeEntry("child", mo.Some("root"), "child", TreeEntryModel, ""),
			testTreeEntry("leaf", mo.Some("child"), "leaf", TreeEntryUser, ""),
		},
		ActiveLeafID: mo.Some("leaf"),
	}, TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)

	// Act by projecting visible topology.
	rows := panel.VisibleRows()

	// Assert no row requires a sibling continuation.
	suite.Require().Equal([]int{0, 1, 2}, lo.Map(rows, func(row TreeRow, _ int) int { return row.Depth }))
	suite.Require().Empty(rows[0].AncestorContinues)
	suite.Require().Empty(rows[1].AncestorContinues)
	suite.Require().Equal([]bool{false}, rows[2].AncestorContinues)
	suite.Require().False(rows[0].HasFollowingSibling)
	suite.Require().False(rows[1].HasFollowingSibling)
	suite.Require().False(rows[2].HasFollowingSibling)
}

// TestBranchedTreeTopologyMarksOnlyRequiredContinuations verifies sibling topology metadata.
func (suite *TreePanelSuite) TestBranchedTreeTopologyMarksOnlyRequiredContinuations() {
	// Arrange a root with two children and one grandchild under the first child.
	panel := NewTreePanel(SessionTree{
		Entries: []TreeEntry{
			testTreeEntry("root", mo.None[string](), "root", TreeEntryUser, ""),
			testTreeEntry("first", mo.Some("root"), "first", TreeEntryModel, ""),
			testTreeEntry("nested", mo.Some("first"), "nested", TreeEntryUser, ""),
			testTreeEntry("last", mo.Some("root"), "last", TreeEntryUser, ""),
		},
		ActiveLeafID: mo.Some("last"),
	}, TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)

	// Act by projecting visible topology.
	rows := panel.VisibleRows()

	// Assert the first child and its descendant carry the only required continuation.
	suite.Require().Empty(rows[1].AncestorContinues)
	suite.Require().True(rows[1].HasFollowingSibling)
	suite.Require().Equal([]bool{true}, rows[2].AncestorContinues)
	suite.Require().False(rows[2].HasFollowingSibling)
	suite.Require().Empty(rows[3].AncestorContinues)
	suite.Require().False(rows[3].HasFollowingSibling)
}

// TestSearchRetainsAncestorContext verifies AND-token search and branch context.
func (suite *TreePanelSuite) TestSearchRetainsAncestorContext() {
	// Arrange a full tree selector.
	panel := NewTreePanel(testSessionTree(), TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)

	// Act by searching branch-summary kind and content fields.
	panel.SetQuery("branch discarded")
	rows := panel.VisibleRows()

	// Assert the matching summary and its ancestors remain visible.
	suite.Require().
		Equal([]string{"root", "model", "summary"}, lo.Map(rows, func(row TreeRow, _ int) string { return row.Entry.ID }))
	suite.Require().True(rows[0].Context)
	suite.Require().True(rows[1].Context)
	suite.Require().False(rows[2].Context)
}

// TestFiltersApplyDocumentedEntryVisibility verifies every tree filter mode.
func (suite *TreePanelSuite) TestFiltersApplyDocumentedEntryVisibility() {
	// Arrange one panel containing every public tree entry kind.
	panel := NewTreePanel(testSessionTree(), TreePurposeNavigate)

	for _, testCase := range []struct {
		name     string
		filter   TreeFilter
		expected []string
	}{
		{name: "default", filter: TreeFilterDefault, expected: []string{"root", "model", "summary", "alternate"}},
		{name: "no tools", filter: TreeFilterNoTools, expected: []string{"root", "model", "summary", "alternate"}},
		{name: "user only", filter: TreeFilterUserOnly, expected: []string{"root", "alternate"}},
		{name: "labeled only", filter: TreeFilterLabeledOnly, expected: []string{"model"}},
		{name: "all", filter: TreeFilterAll, expected: []string{"root", "model", "extension", "summary", "alternate"}},
	} {
		suite.Run(testCase.name, func() {
			// Act by applying one local filter.
			panel.SetFilter(testCase.filter)

			// Assert only the documented entry kinds remain.
			suite.Require().
				Equal(testCase.expected, lo.Map(panel.VisibleRows(), func(row TreeRow, _ int) string { return row.Entry.ID }))
		})
	}
}

// TestFoldPreservesSelectionWhenVisible verifies branch folding is local.
func (suite *TreePanelSuite) TestFoldPreservesSelectionWhenVisible() {
	// Arrange selection on a branch parent.
	panel := NewTreePanel(testSessionTree(), TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)
	panel.SelectedID = mo.Some("model")

	// Act by folding the selected branch.
	panel.ToggleFold()

	// Assert descendants are hidden and the selected parent remains selected.
	suite.Require().
		Equal([]string{"root", "model"}, lo.Map(panel.VisibleRows(), func(row TreeRow, _ int) string { return row.Entry.ID }))
	suite.Require().Equal(mo.Some("model"), panel.SelectedID)
	suite.Require().Contains(panel.Folded, "model")
}

// TestReconcileRemovesStalePresentationReferences verifies durable response reconciliation.
func (suite *TreePanelSuite) TestReconcileRemovesStalePresentationReferences() {
	// Arrange local state that references a branch removed by a durable replacement.
	panel := NewTreePanel(testSessionTree(), TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)
	panel.SelectedID = mo.Some("summary")
	panel.Folded["model"] = struct{}{}

	// Act by reconciling a committed tree that retains only the alternate root branch.
	panel.Reconcile(SessionTree{
		Entries:      []TreeEntry{testTreeEntry("alternate", mo.None[string](), "other prompt", TreeEntryUser, "")},
		ActiveLeafID: mo.Some("alternate"),
	})

	// Assert removed selection and fold references do not survive.
	suite.Require().Equal(mo.Some("alternate"), panel.SelectedID)
	suite.Require().Empty(panel.Folded)
	suite.Require().
		Equal([]string{"alternate"}, lo.Map(panel.VisibleRows(), func(row TreeRow, _ int) string { return row.Entry.ID }))
}

// testSessionTree creates a branched public tree for presentation tests.
func testSessionTree() SessionTree {
	return SessionTree{
		Entries: []TreeEntry{
			testTreeEntry("root", mo.None[string](), "initial prompt", TreeEntryUser, ""),
			testTreeEntry("model", mo.Some("root"), "assistant answer", TreeEntryModel, "checkpoint"),
			testTreeEntry("extension", mo.Some("model"), "extension audit", TreeEntryExtension, ""),
			testTreeEntry("summary", mo.Some("model"), "discarded branch", TreeEntryBranchSummary, ""),
			testTreeEntry("alternate", mo.Some("model"), "other prompt", TreeEntryUser, ""),
		},
		ActiveLeafID: mo.Some("summary"),
	}
}

// testTreeEntry creates one complete public tree entry.
func testTreeEntry(id string, parentID mo.Option[string], text string, kind TreeEntryKind, label string) TreeEntry {
	return TreeEntry{ID: id, ParentID: parentID, CreatedAt: time.Unix(1, 0).UTC(), Label: label, Kind: kind, Text: text}
}
