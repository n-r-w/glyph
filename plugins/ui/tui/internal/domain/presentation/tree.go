package presentation

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
)

const (
	// unknownTreeValue identifies an unspecified tree value.
	unknownTreeValue = "unknown"
	// treeSearchFieldSeparator separates public searchable fields.
	treeSearchFieldSeparator = " "
)

const (
	// treeEntryUserSearchText identifies user entries during search.
	treeEntryUserSearchText = "user"
	// treeEntryModelSearchText identifies model entries during search.
	treeEntryModelSearchText = "assistant model"
	// treeEntryToolResultSearchText identifies tool-result entries during search.
	treeEntryToolResultSearchText = "tool result"
	// treeEntryExtensionSearchText identifies extension entries during search.
	treeEntryExtensionSearchText = "extension"
	// treeEntryBranchSummarySearchText identifies branch summaries during search.
	treeEntryBranchSummarySearchText = "branch summary"
)

// SummaryMode identifies branch-summary behavior for tree navigation.
type SummaryMode int

const (
	// SummaryModeUnspecified identifies a missing branch-summary choice.
	SummaryModeUnspecified SummaryMode = iota
	// SummaryModeNoSummary navigates without creating a branch summary.
	SummaryModeNoSummary
	// SummaryModeSummarize uses the built-in branch summarizer.
	SummaryModeSummarize
	// SummaryModeCustomFocus uses the built-in summarizer with a custom focus.
	SummaryModeCustomFocus
)

// TreeNavigationStatus identifies one navigation terminal result.
type TreeNavigationStatus int

const (
	// TreeNavigationUnspecified identifies a missing navigation result.
	TreeNavigationUnspecified TreeNavigationStatus = iota
	// TreeNavigationCommitted identifies a durable navigation commit.
	TreeNavigationCommitted
	// TreeNavigationCanceled identifies navigation canceled before commit.
	TreeNavigationCanceled
)

// OperationIssue contains one safe nonterminal Host issue.
type OperationIssue struct {
	// Code is the stable issue code.
	Code string
	// ExtensionID identifies the extension when present.
	ExtensionID string
	// HandlerID identifies the handler when present.
	HandlerID string
	// Message is the safe Host-owned issue text.
	Message string
}

// TreeEvent contains one mapped Host tree result.
type TreeEvent struct {
	// Tree contains a committed snapshot when the Host returned one.
	Tree mo.Option[SessionTree]
	// NavigationStatus identifies committed or canceled navigation.
	NavigationStatus TreeNavigationStatus
	// SessionInfo identifies a durable replacement session when present.
	SessionInfo mo.Option[SessionInfo]
	// RestoredTranscript contains a committed active branch when present.
	RestoredTranscript []Line
	// NextInput preserves exact optional editor text.
	NextInput mo.Option[string]
	// Issues contains safe ordered operation issues.
	Issues []OperationIssue
	// FailureMessage contains a safe rejected-operation message.
	FailureMessage mo.Option[string]
}

// TreeCommand contains one tree command payload.
type TreeCommand struct {
	// TargetEntryID identifies the selected entry when required.
	TargetEntryID mo.Option[string]
	// SummaryMode identifies navigation summary behavior.
	SummaryMode SummaryMode
	// CustomFocus preserves exact custom summary focus when present.
	CustomFocus mo.Option[string]
	// Label preserves an explicitly empty label for clearing.
	Label mo.Option[string]
}

// TreeEntryKind identifies one public session-tree entry payload.
type TreeEntryKind int

const (
	// TreeEntryUnspecified identifies a missing tree entry kind.
	TreeEntryUnspecified TreeEntryKind = iota
	// TreeEntryUser identifies a user message.
	TreeEntryUser
	// TreeEntryModel identifies a model response.
	TreeEntryModel
	// TreeEntryToolResult identifies a tool result.
	TreeEntryToolResult
	// TreeEntryExtension identifies an opaque extension entry.
	TreeEntryExtension
	// TreeEntryBranchSummary identifies an abandoned-branch summary.
	TreeEntryBranchSummary
)

// TreeFilter identifies one local tree visibility filter.
type TreeFilter int

const (
	// TreeFilterDefault hides opaque extension entries.
	TreeFilterDefault TreeFilter = iota
	// TreeFilterNoTools also hides tool results.
	TreeFilterNoTools
	// TreeFilterUserOnly shows user messages.
	TreeFilterUserOnly
	// TreeFilterLabeledOnly shows labeled entries.
	TreeFilterLabeledOnly
	// TreeFilterAll shows every entry.
	TreeFilterAll
)

// TreePurpose identifies the operation started by tree target selection.
type TreePurpose int

const (
	// TreePurposeNavigate selects a navigation target.
	TreePurposeNavigate TreePurpose = iota
	// TreePurposeFork selects a user message for a replacement-session fork.
	TreePurposeFork
)

// TreeEntry contains one public tree entry and presentation text.
type TreeEntry struct {
	// ID is the persisted entry identifier.
	ID string
	// ParentID identifies the parent entry when present.
	ParentID mo.Option[string]
	// CreatedAt is the persisted entry timestamp.
	CreatedAt time.Time
	// Label is the committed user label. An empty value means no label.
	Label string
	// Kind identifies the entry payload.
	Kind TreeEntryKind
	// Text contains the public entry text used for rendering and search.
	Text string
}

// SessionTree contains one complete public tree snapshot.
type SessionTree struct {
	// Entries are ordered by Host persistence order.
	Entries []TreeEntry
	// ActiveLeafID identifies the current leaf when present.
	ActiveLeafID mo.Option[string]
}

// TreeRow contains one visible tree row and local presentation metadata.
type TreeRow struct {
	// Entry is the visible public tree entry.
	Entry TreeEntry
	// Depth is the entry depth in the visible tree.
	Depth int
	// AncestorContinues marks non-root visible ancestor levels that have a following sibling.
	AncestorContinues []bool
	// HasFollowingSibling reports whether this row has a later visible sibling.
	HasFollowingSibling bool
	// ActivePath reports whether the entry belongs to the active branch.
	ActivePath bool
	// ActiveLeaf reports whether the entry is the active leaf.
	ActiveLeaf bool
	// Context reports whether search retained this nonmatching ancestor.
	Context bool
	// Folded reports whether descendants are hidden locally.
	Folded bool
	// HasChildren reports whether the complete tree contains direct children.
	HasChildren bool
}

// TreePanel contains client-local tree interaction state.
type TreePanel struct {
	// Tree is the latest committed Host snapshot.
	Tree SessionTree
	// Purpose identifies the command issued after target confirmation.
	Purpose TreePurpose
	// Filter is the active entry visibility filter.
	Filter TreeFilter
	// Query contains the local case-insensitive search query.
	Query string
	// Folded contains locally folded entry identifiers.
	Folded map[string]struct{}
	// SelectedID identifies the selected visible entry when present.
	SelectedID mo.Option[string]
}

// NewTreePanel creates local interaction state from one committed snapshot.
func NewTreePanel(tree SessionTree, purpose TreePurpose) TreePanel {
	panel := TreePanel{
		Tree:       cloneSessionTree(tree),
		Purpose:    purpose,
		Filter:     TreeFilterDefault,
		Query:      "",
		Folded:     map[string]struct{}{},
		SelectedID: tree.ActiveLeafID,
	}
	panel.reconcileSelection()
	return panel
}

// VisibleRows returns the current visible tree projection.
func (panel TreePanel) VisibleRows() []TreeRow {
	entriesByID, childrenByID := panel.treeIndexes()
	activePath := treePathSet(panel.Tree.ActiveLeafID, entriesByID)
	queryTokens := strings.Fields(strings.ToLower(panel.Query))
	directMatches := make(map[string]struct{}, len(panel.Tree.Entries))
	visible := make(map[string]struct{}, len(panel.Tree.Entries))

	for _, entry := range panel.Tree.Entries {
		if !panel.filterMatches(entry) || !searchMatches(entry, queryTokens) {
			continue
		}
		directMatches[entry.ID] = struct{}{}
		visible[entry.ID] = struct{}{}
		if len(queryTokens) > 0 {
			for parentID, present := entry.ParentID.Get(); present; {
				visible[parentID] = struct{}{}
				parent, found := entriesByID[parentID]
				if !found {
					break
				}
				parentID, present = parent.ParentID.Get()
			}
		}
	}

	visibleEntries := make([]TreeEntry, 0, len(visible))
	visibleIDs := make(map[string]struct{}, len(visible))
	for _, entry := range panel.Tree.Entries {
		if _, included := visible[entry.ID]; !included || panel.hiddenByFold(entry, entriesByID) {
			continue
		}
		visibleEntries = append(visibleEntries, entry)
		visibleIDs[entry.ID] = struct{}{}
	}

	visibleParents := make(map[string]string, len(visibleEntries))
	visibleChildren := make(map[string][]string, len(visibleEntries))
	for _, entry := range visibleEntries {
		parentID, present := nearestVisibleParent(entry, entriesByID, visibleIDs)
		if !present {
			continue
		}
		visibleParents[entry.ID] = parentID
		visibleChildren[parentID] = append(visibleChildren[parentID], entry.ID)
	}
	followingSiblings := make(map[string]struct{}, len(visibleEntries))
	for _, childIDs := range visibleChildren {
		for _, childID := range childIDs[:max(0, len(childIDs)-1)] {
			followingSiblings[childID] = struct{}{}
		}
	}

	rows := make([]TreeRow, 0, len(visibleEntries))
	for _, entry := range visibleEntries {
		ancestorIDs := visibleAncestorIDs(entry.ID, visibleParents)
		continuationIDs := ancestorIDs[min(1, len(ancestorIDs)):]
		ancestorContinues := lo.Map(continuationIDs, func(ancestorID string, _ int) bool {
			_, continues := followingSiblings[ancestorID]
			return continues
		})
		_, direct := directMatches[entry.ID]
		_, onActivePath := activePath[entry.ID]
		_, folded := panel.Folded[entry.ID]
		_, hasFollowingSibling := followingSiblings[entry.ID]
		rows = append(rows, TreeRow{
			Entry:               entry,
			Depth:               len(ancestorIDs),
			AncestorContinues:   ancestorContinues,
			HasFollowingSibling: hasFollowingSibling,
			ActivePath:          onActivePath,
			ActiveLeaf:          panel.Tree.ActiveLeafID == mo.Some(entry.ID),
			Context:             !direct,
			Folded:              folded,
			HasChildren:         len(childrenByID[entry.ID]) > 0,
		})
	}
	return rows
}

// nearestVisibleParent returns the closest visible ancestor of one entry.
func nearestVisibleParent(
	entry TreeEntry,
	entriesByID map[string]TreeEntry,
	visibleIDs map[string]struct{},
) (string, bool) {
	for parentID, present := entry.ParentID.Get(); present; {
		if _, visible := visibleIDs[parentID]; visible {
			return parentID, true
		}
		parent, found := entriesByID[parentID]
		if !found {
			return "", false
		}
		parentID, present = parent.ParentID.Get()
	}
	return "", false
}

// visibleAncestorIDs returns visible ancestors in root-to-parent order.
func visibleAncestorIDs(entryID string, visibleParents map[string]string) []string {
	ancestors := make([]string, 0)
	for parentID, present := visibleParents[entryID]; present; parentID, present = visibleParents[parentID] {
		ancestors = append(ancestors, parentID)
	}
	slices.Reverse(ancestors)
	return ancestors
}

// SetQuery changes local search state and reconciles selection.
func (panel *TreePanel) SetQuery(query string) {
	panel.Query = query
	panel.reconcileSelection()
}

// SetFilter changes local visibility and reconciles selection.
func (panel *TreePanel) SetFilter(filter TreeFilter) {
	panel.Filter = filter
	panel.reconcileSelection()
}

// MoveSelection moves selection by one visible row with clamping.
func (panel *TreePanel) MoveSelection(delta int) {
	rows := panel.VisibleRows()
	if len(rows) == 0 {
		panel.SelectedID = mo.None[string]()
		return
	}
	index := 0
	if selectedID, present := panel.SelectedID.Get(); present {
		if selectedIndex := slices.IndexFunc(
			rows,
			func(row TreeRow) bool { return row.Entry.ID == selectedID },
		); selectedIndex >= 0 {
			index = selectedIndex
		}
	}
	index = max(0, min(len(rows)-1, index+delta))
	panel.SelectedID = mo.Some(rows[index].Entry.ID)
}

// ToggleFold changes only local folding state for the selected branch.
func (panel *TreePanel) ToggleFold() {
	selectedID, present := panel.SelectedID.Get()
	if !present {
		return
	}
	_, childrenByID := panel.treeIndexes()
	if len(childrenByID[selectedID]) == 0 {
		return
	}
	if _, folded := panel.Folded[selectedID]; folded {
		delete(panel.Folded, selectedID)
	} else {
		panel.Folded[selectedID] = struct{}{}
	}
	panel.reconcileSelection()
}

// Reconcile applies a committed tree and removes references to absent entries.
func (panel *TreePanel) Reconcile(tree SessionTree) {
	panel.Tree = cloneSessionTree(tree)
	entriesByID, childrenByID := panel.treeIndexes()
	maps.DeleteFunc(panel.Folded, func(id string, _ struct{}) bool {
		_, present := entriesByID[id]
		return !present || len(childrenByID[id]) == 0
	})
	panel.reconcileSelection()
}

// filterMatches reports whether one entry belongs to the active filter.
func (panel TreePanel) filterMatches(entry TreeEntry) bool {
	switch panel.Filter {
	case TreeFilterDefault:
		return entry.Kind != TreeEntryExtension
	case TreeFilterNoTools:
		return entry.Kind != TreeEntryExtension && entry.Kind != TreeEntryToolResult
	case TreeFilterUserOnly:
		return entry.Kind == TreeEntryUser
	case TreeFilterLabeledOnly:
		return entry.Label != ""
	case TreeFilterAll:
		return true
	default:
		return false
	}
}

// treeIndexes builds lookup data for one validated Host snapshot.
func (panel TreePanel) treeIndexes() (
	entriesByID map[string]TreeEntry,
	childrenByID map[string][]string,
) {
	entriesByID = make(map[string]TreeEntry, len(panel.Tree.Entries))
	childrenByID = make(map[string][]string, len(panel.Tree.Entries))
	for _, entry := range panel.Tree.Entries {
		entriesByID[entry.ID] = entry
		if parentID, present := entry.ParentID.Get(); present {
			childrenByID[parentID] = append(childrenByID[parentID], entry.ID)
		}
	}
	return entriesByID, childrenByID
}

// hiddenByFold reports whether any ancestor is locally folded.
func (panel TreePanel) hiddenByFold(entry TreeEntry, entriesByID map[string]TreeEntry) bool {
	parentID, present := entry.ParentID.Get()
	for present {
		if _, folded := panel.Folded[parentID]; folded {
			return true
		}
		parent, found := entriesByID[parentID]
		if !found {
			return false
		}
		parentID, present = parent.ParentID.Get()
	}
	return false
}

// reconcileSelection keeps a visible selection or chooses its nearest visible ancestor.
func (panel *TreePanel) reconcileSelection() {
	rows := panel.VisibleRows()
	if len(rows) == 0 {
		panel.SelectedID = mo.None[string]()
		return
	}
	visibleIDs := make(map[string]struct{}, len(rows))
	for index := range rows {
		visibleIDs[rows[index].Entry.ID] = struct{}{}
	}
	entriesByID, _ := panel.treeIndexes()
	if selectedID, present := panel.SelectedID.Get(); present {
		for {
			if _, visible := visibleIDs[selectedID]; visible {
				panel.SelectedID = mo.Some(selectedID)
				return
			}
			entry, found := entriesByID[selectedID]
			if !found {
				break
			}
			parentID, hasParent := entry.ParentID.Get()
			if !hasParent {
				break
			}
			selectedID = parentID
		}
	}
	if activeLeafID, present := panel.Tree.ActiveLeafID.Get(); present {
		if _, visible := visibleIDs[activeLeafID]; visible {
			panel.SelectedID = mo.Some(activeLeafID)
			return
		}
	}
	panel.SelectedID = mo.Some(rows[0].Entry.ID)
}

// searchMatches applies case-insensitive AND-token matching to public fields.
func searchMatches(entry TreeEntry, tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	haystack := strings.ToLower(
		strings.Join(
			[]string{entry.ID, entry.Label, treeEntryKindText(entry.Kind), entry.Text},
			treeSearchFieldSeparator,
		),
	)
	for _, token := range tokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

// treeEntryKindText returns the stable visible name for one tree entry kind.
func treeEntryKindText(kind TreeEntryKind) string {
	switch kind {
	case TreeEntryUser:
		return treeEntryUserSearchText
	case TreeEntryModel:
		return treeEntryModelSearchText
	case TreeEntryToolResult:
		return treeEntryToolResultSearchText
	case TreeEntryExtension:
		return treeEntryExtensionSearchText
	case TreeEntryBranchSummary:
		return treeEntryBranchSummarySearchText
	case TreeEntryUnspecified:
		return unknownTreeValue
	default:
		return unknownTreeValue
	}
}

// treePathSet returns every entry from one leaf through the root.
func treePathSet(leafID mo.Option[string], entriesByID map[string]TreeEntry) map[string]struct{} {
	path := make(map[string]struct{})
	currentID, present := leafID.Get()
	for present {
		path[currentID] = struct{}{}
		entry, found := entriesByID[currentID]
		if !found {
			break
		}
		currentID, present = entry.ParentID.Get()
	}
	return path
}

// cloneSessionTree prevents local presentation state from aliasing mapped frames.
func cloneSessionTree(tree SessionTree) SessionTree {
	return SessionTree{Entries: slices.Clone(tree.Entries), ActiveLeafID: tree.ActiveLeafID}
}
