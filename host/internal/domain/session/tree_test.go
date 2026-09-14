//go:build !integration

package session

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestTreeImplicitRootConstructionAndClonePreserveStoredState verifies an absent active position keeps the full tree.
func TestTreeImplicitRootConstructionAndClonePreserveStoredState(t *testing.T) {
	t.Parallel()

	// Arrange a stored branch and label whose active position is the implicit root.
	createdAt := time.Unix(1, 0).UTC()
	entries := []Entry{
		treeUserEntry("root", mo.None[string](), "root input", createdAt),
		treeModelEntry("model", mo.Some("root"), createdAt.Add(time.Second)),
	}
	labels := map[string]string{"root": "kept"}

	// Act by constructing and cloning the complete stored tree.
	tree, err := NewTree(entries, mo.None[string](), labels)
	require.NoError(t, err)
	clone := tree.Clone()

	// Assert both trees retain stored state while exposing an empty active branch.
	for _, candidate := range []Tree{tree, clone} {
		require.Equal(t, entries, candidate.Entries())
		require.Equal(t, labels, candidate.Labels())
		require.True(t, candidate.ActiveLeafID().IsNone())
		require.Empty(t, candidate.ActiveBranch())
	}
}

// TestTreeImplicitRootNavigationPreparesExactInput verifies root user and extension content target the implicit root.
func TestTreeImplicitRootNavigationPreparesExactInput(t *testing.T) {
	t.Parallel()

	createdAt := time.Unix(1, 0).UTC()
	for _, test := range []struct {
		name          string
		entry         Entry
		expectedInput string
	}{
		{
			name:          "user",
			entry:         treeUserEntry("root-user", mo.None[string](), "exact root user input", createdAt),
			expectedInput: "exact root user input",
		},
		{
			name: "model-visible extension message",
			entry: Entry{
				ID: "root-extension", ParentID: mo.None[string](), CreatedAt: createdAt,
				Information: mo.None[Information](), User: mo.None[UserMessage](), Model: mo.None[ModelResponse](),
				EstimatedCost: mo.None[EstimatedCost](), ToolResult: mo.None[ToolResult](),
				Extension: mo.None[ExtensionEnvelope](), ExtensionMessage: mo.Some(ExtensionMessage{
					ExtensionID: "example", EntryType: "note", Text: "exact\nroot extension input",
					Visibility: ClientVisibilityVisible,
				}), BranchSummary: mo.None[BranchSummaryEntry](),
			},
			expectedInput: "exact\nroot extension input",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one root entry as the current active leaf.
			tree, err := NewTree([]Entry{test.entry}, mo.Some(test.entry.ID), nil)
			require.NoError(t, err)

			// Act by selecting the root content.
			preparation, err := tree.NavigationPreparation(test.entry.ID)

			// Assert navigation selects the implicit root and preserves exact input text.
			require.NoError(t, err)
			require.True(t, preparation.DestinationID.IsNone())
			require.Equal(t, mo.Some(test.expectedInput), preparation.NextInput)
		})
	}
}

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
	require.EqualError(t, err, "active leaf does not exist")

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
	require.Equal(t, []string{"root", "model-a", "user-b", "model-b"}, lo.Map(branch, func(entry Entry, _ int) string {
		return entry.ID
	}))
	require.Equal(t, mo.Some("model-a"), preparation.DestinationID)
	require.Equal(t, mo.Some("edit this exactly"), preparation.NextInput)
	require.Equal(t, mo.Some("model-a"), preparation.CommonAncestorID)
	require.Equal(t, []string{"user-b", "model-b"}, lo.Map(preparation.AbandonedPath, func(entry Entry, _ int) string {
		return entry.ID
	}))
	require.Equal(t, map[string]string{"model-a": "checkpoint"}, tree.Labels())
}

// TestTreeNavigationPreparesExtensionMessageInput verifies both client visibility values use message text and parent.
func TestTreeNavigationPreparesExtensionMessageInput(t *testing.T) {
	t.Parallel()

	// Arrange model-visible extension messages with both client visibility values.
	createdAt := time.Unix(1, 0).UTC()
	root := treeUserEntry("root", mo.None[string](), "root", createdAt)
	for _, visibility := range []ClientVisibility{ClientVisibilityVisible, ClientVisibilityHidden} {
		t.Run(string(visibility), func(t *testing.T) {
			t.Parallel()
			message := Entry{
				ID: "message-" + string(visibility), ParentID: mo.Some("root"), CreatedAt: createdAt.Add(time.Second),
				Information: mo.None[Information](), User: mo.None[UserMessage](), Model: mo.None[ModelResponse](),
				EstimatedCost: mo.None[EstimatedCost](), ToolResult: mo.None[ToolResult](),
				Extension: mo.None[ExtensionEnvelope](), ExtensionMessage: mo.Some(ExtensionMessage{
					ExtensionID: "example", EntryType: "note", Text: "exact\nmessage", Visibility: visibility,
				}), BranchSummary: mo.None[BranchSummaryEntry](),
			}
			tree, err := NewTree([]Entry{root, message}, mo.Some(message.ID), nil)
			require.NoError(t, err)

			// Act by selecting the extension message.
			preparation, err := tree.NavigationPreparation(message.ID)

			// Assert the parent is the destination and exact text is the next input.
			require.NoError(t, err)
			require.Equal(t, mo.Some("root"), preparation.DestinationID)
			require.Equal(t, mo.Some("exact\nmessage"), preparation.NextInput)
		})
	}
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
	require.Equal(t, []string{"root", "old", "new"}, lo.Map(tree.Entries(), func(entry Entry, _ int) string {
		return entry.ID
	}))
	require.Equal(t, mo.Some("new"), tree.ActiveLeafID())
}

func treeUserEntry(id string, parentID mo.Option[string], text string, createdAt time.Time) Entry {
	return Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[Information](), User: mo.Some(model.TextMessage(text)),
		Model: mo.None[ModelResponse](), EstimatedCost: mo.None[EstimatedCost](),
		ToolResult: mo.None[ToolResult](), Extension: mo.None[ExtensionEnvelope](),
		ExtensionMessage: mo.None[ExtensionMessage](), BranchSummary: mo.None[BranchSummaryEntry](),
	}
}

func treeModelEntry(id string, parentID mo.Option[string], createdAt time.Time) Entry {
	return Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[Information](), User: mo.None[UserMessage](),
		Model: mo.Some(model.Response{}), EstimatedCost: mo.None[EstimatedCost](),
		ToolResult: mo.None[ToolResult](), Extension: mo.None[ExtensionEnvelope](),
		ExtensionMessage: mo.None[ExtensionMessage](), BranchSummary: mo.None[BranchSummaryEntry](),
	}
}
