package session

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestTreeActiveBranchAndNavigationPreparation verifies branch projection and user-target navigation semantics.
func TestTreeActiveBranchAndNavigationPreparation(t *testing.T) {
	t.Parallel()

	// Arrange a tree with one abandoned model branch and one active continuation.
	createdAt := time.Unix(1, 0).UTC()
	root := treeUserEntry("root", mo.None[string](), "root input", createdAt)
	firstModel := treeModelEntry("model-a", mo.Some("root"), createdAt.Add(time.Second))
	activeUser := treeUserEntry("user-b", mo.Some("model-a"), "edit this exactly", createdAt.Add(2*time.Second))
	activeModel := treeModelEntry("model-b", mo.Some("user-b"), createdAt.Add(3*time.Second))
	alternate := treeModelEntry("model-c", mo.Some("root"), createdAt.Add(4*time.Second))
	_, err := NewTree(
		[]Entry{root, firstModel, activeUser, activeModel, alternate},
		mo.Some("active-label-placeholder"),
		map[string]string{"model-a": "checkpoint"},
	)
	require.Error(t, err)

	// Arrange the valid active leaf after proving invalid active-leaf validation.
	tree, err := NewTree(
		[]Entry{root, firstModel, activeUser, activeModel, alternate},
		mo.Some("model-b"),
		map[string]string{"model-a": "checkpoint"},
	)
	require.NoError(t, err)

	// Act by projecting the active branch and preparing navigation to the active user message.
	branch := tree.ActiveBranch()
	preparation, err := tree.NavigationPreparation("user-b")

	// Assert root-first order, exact editable input, destination, common ancestor, and abandoned path.
	require.NoError(t, err)
	require.Equal(t, []string{"root", "model-a", "user-b", "model-b"}, entryIDs(branch))
	require.Equal(t, mo.Some("model-a"), preparation.DestinationID)
	require.Equal(t, mo.Some("edit this exactly"), preparation.NextInput)
	require.Equal(t, mo.Some("model-a"), preparation.CommonAncestorID)
	require.Equal(t, []string{"user-b", "model-b"}, entryIDs(preparation.AbandonedPath))
	require.Equal(t, map[string]string{"model-a": "checkpoint"}, tree.Labels())
}

// TestTreeAddPreservesBranchesAndValidatesParent verifies append-only branch insertion.
func TestTreeAddPreservesBranchesAndValidatesParent(t *testing.T) {
	t.Parallel()

	// Arrange one root and one existing child branch.
	createdAt := time.Unix(1, 0).UTC()
	tree, err := NewTree([]Entry{
		treeUserEntry("root", mo.None[string](), "root", createdAt),
		treeModelEntry("old", mo.Some("root"), createdAt.Add(time.Second)),
	}, mo.Some("old"), nil)
	require.NoError(t, err)

	// Act by adding a sibling branch, then trying an entry with an unknown parent.
	require.NoError(t, tree.Add(treeModelEntry("new", mo.Some("root"), createdAt.Add(2*time.Second))))
	err = tree.Add(treeModelEntry("invalid", mo.Some("missing"), createdAt.Add(3*time.Second)))

	// Assert the invalid append is rejected and both valid branches remain.
	require.Error(t, err)
	require.Equal(t, []string{"root", "old", "new"}, entryIDs(tree.Entries()))
	require.Equal(t, mo.Some("new"), tree.ActiveLeafID())
}

func treeUserEntry(id string, parentID mo.Option[string], text string, createdAt time.Time) Entry {
	return Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[Information](), User: mo.Some(model.TextMessage(text)),
		Model: mo.None[ModelResponse](), EstimatedCost: mo.None[EstimatedCost](),
		ToolResult: mo.None[ToolResult](), Extension: mo.None[ExtensionEnvelope](),
		BranchSummary: mo.None[BranchSummaryEntry](),
	}
}

func treeModelEntry(id string, parentID mo.Option[string], createdAt time.Time) Entry {
	return Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[Information](), User: mo.None[UserMessage](),
		Model: mo.Some(model.Response{}), EstimatedCost: mo.None[EstimatedCost](),
		ToolResult: mo.None[ToolResult](), Extension: mo.None[ExtensionEnvelope](),
		BranchSummary: mo.None[BranchSummaryEntry](),
	}
}

func entryIDs(entries []Entry) []string {
	ids := make([]string, len(entries))
	for index := range entries {
		ids[index] = entries[index].ID
	}
	return ids
}
