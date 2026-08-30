package session

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// Tree owns parent-linked entries, the active leaf, and entry labels.
type Tree struct {
	// entries preserves persistence order across all branches.
	entries []Entry
	// index resolves entry identity without exposing mutable aggregate state.
	index map[string]int
	// activeLeafID identifies the entry used as parent for the next append.
	activeLeafID mo.Option[string]
	// labels stores the latest nonempty label for each target entry.
	labels map[string]string
}

// NavigationPreparation contains validated state needed to navigate one tree target.
type NavigationPreparation struct {
	// DestinationID identifies the selected conversation position or the implicit root when absent.
	DestinationID mo.Option[string]
	// NextInput contains editable user text when a user message is selected.
	NextInput mo.Option[string]
	// CommonAncestorID identifies the last entry shared by the active and destination paths.
	CommonAncestorID mo.Option[string]
	// AbandonedPath contains active entries after the common ancestor in root-first order.
	AbandonedPath []Entry
}

// NewTree validates and owns one complete session-tree snapshot.
func NewTree(entries []Entry, activeLeafID mo.Option[string], labels map[string]string) (Tree, error) {
	tree := Tree{
		entries: make([]Entry, 0, len(entries)), index: make(map[string]int, len(entries)),
		activeLeafID: mo.None[string](), labels: make(map[string]string, len(labels)),
	}
	for index := range entries {
		if err := tree.add(entries[index], false); err != nil {
			return Tree{}, fmt.Errorf("add session entry: %w", err)
		}
	}
	if activeLeafID.IsSome() {
		id := activeLeafID.OrEmpty()
		if _, exists := tree.index[id]; !exists {
			return Tree{}, errors.New("active leaf does not exist")
		}
		tree.activeLeafID = mo.Some(id)
	} else if len(entries) != 0 {
		return Tree{}, errors.New("nonempty tree requires an active leaf")
	}
	for id, label := range labels {
		if _, exists := tree.index[id]; !exists {
			return Tree{}, fmt.Errorf("label target %q does not exist", id)
		}
		if label != "" {
			tree.labels[id] = label
		}
	}
	return tree, nil
}

// Add validates and appends one entry as the active leaf.
func (tree *Tree) Add(entry Entry) error { return tree.add(entry, true) }

// add validates one append without requiring the new entry to extend the current active branch.
func (tree *Tree) add(entry Entry, advance bool) error {
	if tree.index == nil {
		tree.index = make(map[string]int)
	}
	if tree.labels == nil {
		tree.labels = make(map[string]string)
	}
	if entry.ID == "" {
		return errors.New("entry ID is required")
	}
	if _, exists := tree.index[entry.ID]; exists {
		return errors.New("duplicate entry ID")
	}
	if parentID, present := entry.ParentID.Get(); present {
		if _, exists := tree.index[parentID]; !exists {
			return errors.New("entry parent does not exist")
		}
	}
	payloads := 0
	for _, present := range []bool{
		entry.User.IsSome(), entry.Model.IsSome(), entry.ToolResult.IsSome(),
		entry.Extension.IsSome(), entry.BranchSummary.IsSome(),
	} {
		if present {
			payloads++
		}
	}
	if payloads != 1 || entry.Information.IsSome() || entry.Model.IsNone() && entry.EstimatedCost.IsSome() {
		return errors.New("entry must contain exactly one tree payload")
	}
	owned := entry.Clone()
	tree.index[owned.ID] = len(tree.entries)
	tree.entries = append(tree.entries, owned)
	if advance {
		tree.activeLeafID = mo.Some(owned.ID)
	}
	return nil
}

// Clone returns an independently owned valid tree snapshot.
func (tree Tree) Clone() Tree {
	cloned, err := NewTree(tree.Entries(), tree.ActiveLeafID(), tree.Labels())
	if err != nil {
		panic(err)
	}
	return cloned
}

// ActiveBranch returns the active path in root-first order.
func (tree Tree) ActiveBranch() []Entry {
	path := tree.pathTo(tree.activeLeafID)
	return cloneTreeEntries(path)
}

// BranchTo returns the path through one entry or the implicit root when the ID is absent.
func (tree Tree) BranchTo(id mo.Option[string]) ([]Entry, error) {
	if value, present := id.Get(); present {
		if _, exists := tree.index[value]; !exists {
			return nil, ErrEntryNotFound
		}
	}
	return cloneTreeEntries(tree.pathTo(id)), nil
}

// NavigationPreparation validates one target and derives navigation state.
func (tree Tree) NavigationPreparation(targetID string) (NavigationPreparation, error) {
	targetIndex, exists := tree.index[targetID]
	if !exists {
		return NavigationPreparation{}, fmt.Errorf("prepare navigation: %w", ErrEntryNotFound)
	}
	target := tree.entries[targetIndex]
	destinationID := mo.Some(target.ID)
	nextInput := mo.None[string]()
	if user, present := target.User.Get(); present {
		destinationID = target.ParentID
		nextInput = mo.Some(user.Text("\n"))
	}
	activePath := tree.pathTo(tree.activeLeafID)
	destinationPath := tree.pathTo(destinationID)
	shared := 0
	for shared < len(activePath) && shared < len(destinationPath) && activePath[shared].ID == destinationPath[shared].ID {
		shared++
	}
	commonAncestorID := mo.None[string]()
	if shared > 0 {
		commonAncestorID = mo.Some(activePath[shared-1].ID)
	}
	return NavigationPreparation{
		DestinationID:    destinationID,
		NextInput:        nextInput,
		CommonAncestorID: commonAncestorID,
		AbandonedPath:    cloneTreeEntries(activePath[shared:]),
	}, nil
}

// Entries returns an owned persistence-order snapshot.
func (tree Tree) Entries() []Entry { return cloneTreeEntries(tree.entries) }

// ActiveLeafID returns the current active leaf when the tree is not empty.
func (tree Tree) ActiveLeafID() mo.Option[string] { return tree.activeLeafID }

// Labels returns an owned label snapshot keyed by entry ID.
func (tree Tree) Labels() map[string]string {
	labels := make(map[string]string, len(tree.labels))
	maps.Copy(labels, tree.labels)
	return labels
}

// ValidateSummaryBoundary checks that a branch summary covers one connected ancestor path.
func (tree Tree) ValidateSummaryBoundary(summary BranchSummaryEntry) error {
	firstIndex, firstExists := tree.index[summary.FirstEntryID]
	_, lastExists := tree.index[summary.LastEntryID]
	if !firstExists || !lastExists {
		return nil
	}
	first := tree.entries[firstIndex]
	current := summary.LastEntryID
	for {
		if current == first.ID {
			return nil
		}
		entry := tree.entries[tree.index[current]]
		parent, present := entry.ParentID.Get()
		if !present {
			return errors.New("branch summary boundary is disconnected")
		}
		current = parent
	}
}

// SetActiveLeaf validates and changes the entry used for subsequent appends.
func (tree *Tree) SetActiveLeaf(id mo.Option[string]) error {
	if id.IsNone() {
		tree.activeLeafID = mo.None[string]()
		return nil
	}
	value := id.OrEmpty()
	if _, exists := tree.index[value]; !exists {
		return errors.New("active leaf does not exist")
	}
	tree.activeLeafID = mo.Some(value)
	return nil
}

// SetLabel validates and applies the latest label state for one entry.
func (tree *Tree) SetLabel(id, label string) error {
	if _, exists := tree.index[id]; !exists {
		return errors.New("label target does not exist")
	}
	if label == "" {
		delete(tree.labels, id)
		return nil
	}
	tree.labels[id] = label
	return nil
}

// pathTo walks parent relations and returns one root-first path.
func (tree Tree) pathTo(id mo.Option[string]) []Entry {
	if id.IsNone() {
		return nil
	}
	path := make([]Entry, 0)
	current := id
	for current.IsSome() {
		entry := tree.entries[tree.index[current.OrEmpty()]]
		path = append(path, entry)
		current = entry.ParentID
	}
	slices.Reverse(path)
	return path
}

// cloneTreeEntries prevents snapshots from sharing mutable entry payloads.
func cloneTreeEntries(entries []Entry) []Entry {
	return lo.Map(entries, func(entry Entry, _ int) Entry {
		return entry.Clone()
	})
}

// Clone returns a deep copy of the entry.
func (entry Entry) Clone() Entry {
	entry.User = entry.User.MapValue(model.Message.Clone)
	entry.Model = entry.Model.MapValue(model.Response.Clone)
	entry.ToolResult = entry.ToolResult.MapValue(agent.ToolResult.Clone)
	entry.Extension = entry.Extension.MapValue(ExtensionEnvelope.Clone)
	return entry
}

// Clone returns a deep copy of the extension envelope.
func (extension ExtensionEnvelope) Clone() ExtensionEnvelope {
	extension.Data = bytes.Clone(extension.Data)
	return extension
}
