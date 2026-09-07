package terminal

import (
	"slices"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// treeRow adds terminal connector geometry to one semantically visible entry.
type treeRow struct {
	// VisibleEntry contains application-selected visibility and branch facts.
	presentation.VisibleEntry
	// Depth counts visible ancestors for indentation.
	Depth int
	// AncestorContinues identifies ancestor connector columns with following siblings.
	AncestorContinues []bool
	// HasFollowingSibling selects a middle or final branch connector.
	HasFollowingSibling bool
}

// treeRows computes geometry without choosing visibility or changing selection.
func treeRows(entries []presentation.VisibleEntry) []treeRow {
	parents := make(map[string]string, len(entries))
	children := make(map[string][]string, len(entries))
	for index := range entries {
		entry := &entries[index]
		if parent, present := entry.ParentID.Get(); present {
			parents[entry.Entry.ID] = parent
			children[parent] = append(children[parent], entry.Entry.ID)
		}
	}
	following := make(map[string]bool, len(entries))
	for _, siblings := range children {
		for _, id := range siblings[:max(0, len(siblings)-1)] {
			following[id] = true
		}
	}
	return lo.Map(entries, func(entry presentation.VisibleEntry, _ int) treeRow {
		ancestors := visibleAncestorIDs(entry.Entry.ID, parents)
		continuations := ancestors[min(1, len(ancestors)):]
		return treeRow{
			VisibleEntry: entry, Depth: len(ancestors),
			AncestorContinues:   lo.Map(continuations, func(id string, _ int) bool { return following[id] }),
			HasFollowingSibling: following[entry.Entry.ID],
		}
	})
}

// visibleAncestorIDs returns display ancestors in root-to-parent order.
func visibleAncestorIDs(id string, parents map[string]string) []string {
	ancestors := make([]string, 0)
	for parent, present := parents[id]; present; parent, present = parents[parent] {
		ancestors = append(ancestors, parent)
	}
	slices.Reverse(ancestors)
	return ancestors
}
