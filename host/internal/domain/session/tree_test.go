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

// TestTreeRestoreRejectsCompactionBoundaryInsideHiddenToolGroup verifies persisted markers keep tool groups whole.
func TestTreeRestoreRejectsCompactionBoundaryInsideHiddenToolGroup(t *testing.T) {
	t.Parallel()

	// Arrange a reachable stored branch with a hidden entry between one tool call and its result.
	createdAt := time.Unix(1, 0).UTC()
	arguments, err := model.NewToolCallArguments([]byte(`{}`))
	require.NoError(t, err)
	root := treeUserEntry("root", mo.None[string](), "root", createdAt)
	call := treeToolCallEntry("call", "root", "tool-call", arguments, createdAt.Add(time.Second))
	hidden := treeBaseEntry("hidden", mo.Some("call"), createdAt.Add(2*time.Second))
	hidden.Extension = mo.Some(ExtensionEnvelope{
		ExtensionID: "extension", EntryType: "state", Data: []byte(`{"hidden":true}`),
	})
	result := treeBaseEntry("result", mo.Some("hidden"), createdAt.Add(3*time.Second))
	result.ToolResult = mo.Some(ToolResult{CallID: "tool-call", ToolName: "tool", Contents: nil, IsError: false})
	marker := treeCompactionEntry("compaction", "result", "hidden", createdAt.Add(4*time.Second))

	// Act by restoring the persisted branch and marker.
	_, err = NewTree([]Entry{root, call, hidden, result, marker}, mo.Some("compaction"), nil)

	// Assert restoration rejects a boundary that discards the call while retaining its result.
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool")
}

// TestTreeRestoreRejectsCompactionBoundaryInsideVisibleToolGroup verifies a model-visible extension message does not
// terminate ownership of the actual tool result that follows it.
func TestTreeRestoreRejectsCompactionBoundaryInsideVisibleToolGroup(t *testing.T) {
	t.Parallel()

	// Arrange one tool call, an extension message appended during execution, and the actual result.
	createdAt := time.Unix(1, 0).UTC()
	arguments, err := model.NewToolCallArguments([]byte(`{}`))
	require.NoError(t, err)
	root := treeUserEntry("root", mo.None[string](), "root", createdAt)
	call := treeToolCallEntry("call", "root", "tool-call", arguments, createdAt.Add(time.Second))
	message := treeBaseEntry("message", mo.Some("call"), createdAt.Add(2*time.Second))
	message.ExtensionMessage = mo.Some(ExtensionMessage{
		ExtensionID: "extension", EntryType: "progress", Text: "tool is running",
		Visibility: ClientVisibilityVisible,
	})
	result := treeBaseEntry("result", mo.Some("message"), createdAt.Add(3*time.Second))
	result.ToolResult = mo.Some(ToolResult{CallID: "tool-call", ToolName: "tool", Contents: nil, IsError: false})
	marker := treeCompactionEntry("compaction", "result", "message", createdAt.Add(4*time.Second))

	// Act by restoring a marker that keeps the message and result but discards their owning call.
	_, err = NewTree([]Entry{root, call, message, result, marker}, mo.Some("compaction"), nil)

	// Assert the intervening model-visible message does not make the split boundary valid.
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool")
}

// TestTreeRestoreAssociatesRepeatedToolIDsWithTheirModelResponses verifies tool IDs are local to one response.
func TestTreeRestoreAssociatesRepeatedToolIDsWithTheirModelResponses(t *testing.T) {
	t.Parallel()

	// Arrange two complete tool turns that reuse the same provider-local call ID.
	createdAt := time.Unix(1, 0).UTC()
	arguments, err := model.NewToolCallArguments([]byte(`{}`))
	require.NoError(t, err)
	firstUser := treeUserEntry("u1", mo.None[string](), "first", createdAt)
	firstModel := treeToolCallEntry("m1", "u1", "same", arguments, createdAt.Add(time.Second))
	firstResult := treeBaseEntry("r1", mo.Some("m1"), createdAt.Add(2*time.Second))
	firstResult.ToolResult = mo.Some(ToolResult{CallID: "same", ToolName: "tool", Contents: nil, IsError: false})
	secondUser := treeUserEntry("u2", mo.Some("r1"), "second", createdAt.Add(3*time.Second))
	secondModel := treeToolCallEntry("m2", "u2", "same", arguments, createdAt.Add(4*time.Second))
	secondResult := treeBaseEntry("r2", mo.Some("m2"), createdAt.Add(5*time.Second))
	secondResult.ToolResult = mo.Some(ToolResult{CallID: "same", ToolName: "tool", Contents: nil, IsError: false})
	marker := treeCompactionEntry("compaction", "r2", "m2", createdAt.Add(6*time.Second))

	// Act by restoring a compaction that retains the complete second turn.
	tree, err := NewTree(
		[]Entry{firstUser, firstModel, firstResult, secondUser, secondModel, secondResult, marker},
		mo.Some("compaction"),
		nil,
	)

	// Assert the first turn's equal call ID is not treated as owner of the retained result.
	require.NoError(t, err)
	require.Len(t, tree.ActiveBranch(), 7)
}

// TestTreeRestoreAllowsCompactionAfterFailedOrAbortedCalls verifies model-hidden calls do not imply runtime work.
func TestTreeRestoreAllowsCompactionAfterFailedOrAbortedCalls(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		outcome model.Outcome
	}{
		{name: "failed", outcome: model.OutcomeFailed},
		{name: "aborted", outcome: model.OutcomeAborted},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a stored failed or aborted response whose finalized call is excluded from model-visible history.
			createdAt := time.Unix(1, 0).UTC()
			arguments, err := model.NewToolCallArguments([]byte(`{}`))
			require.NoError(t, err)
			root := treeUserEntry("root", mo.None[string](), "root", createdAt)
			call := treeToolCallEntry("call", "root", "tool-call", arguments, createdAt.Add(time.Second))
			response := call.Model.MustGet()
			response.Outcome = mo.Some(test.outcome)
			call.Model = mo.Some(response)
			later := treeUserEntry("later", mo.Some("call"), "later", createdAt.Add(2*time.Second))
			marker := treeCompactionEntry("compaction", "later", "later", createdAt.Add(3*time.Second))

			// Act by restoring a compaction after the model-hidden failed response.
			tree, err := NewTree([]Entry{root, call, later, marker}, mo.Some("compaction"), nil)

			// Assert absent results do not permanently block compaction.
			require.NoError(t, err)
			require.Len(t, tree.ActiveBranch(), 4)
		})
	}
}

// TestTreeRestoreRejectsBoundaryBeforeLatestCompaction verifies repeated compaction never reintroduces summarized entries.
func TestTreeRestoreRejectsBoundaryBeforeLatestCompaction(t *testing.T) {
	t.Parallel()

	// Arrange a stored branch whose second marker moves backward before the first marker's retained boundary.
	createdAt := time.Unix(1, 0).UTC()
	entries := []Entry{
		treeUserEntry("u1", mo.None[string](), "summarized", createdAt),
		treeUserEntry("u2", mo.Some("u1"), "first retained", createdAt.Add(time.Second)),
		treeCompactionEntry("c1", "u2", "u2", createdAt.Add(2*time.Second)),
		treeUserEntry("u3", mo.Some("c1"), "later retained", createdAt.Add(3*time.Second)),
		treeCompactionEntry("c2", "u3", "u1", createdAt.Add(4*time.Second)),
	}

	// Act by restoring the reachable repeated-compaction sequence.
	_, err := NewTree(entries, mo.Some("c2"), nil)

	// Assert the later marker cannot move its retained boundary backward.
	require.Error(t, err)
	require.Contains(t, err.Error(), "preceding compaction")
}

// TestTreeRestoreAcceptsSameAndForwardRepeatedCompactionBoundaries verifies monotonic retained boundaries.
func TestTreeRestoreAcceptsSameAndForwardRepeatedCompactionBoundaries(t *testing.T) {
	t.Parallel()

	for _, firstKeptID := range []string{"u2", "u3"} {
		t.Run(firstKeptID, func(t *testing.T) {
			t.Parallel()

			// Arrange a second marker at the prior boundary or a later retained entry.
			createdAt := time.Unix(1, 0).UTC()
			entries := []Entry{
				treeUserEntry("u1", mo.None[string](), "summarized", createdAt),
				treeUserEntry("u2", mo.Some("u1"), "first retained", createdAt.Add(time.Second)),
				treeCompactionEntry("c1", "u2", "u2", createdAt.Add(2*time.Second)),
				treeUserEntry("u3", mo.Some("c1"), "later retained", createdAt.Add(3*time.Second)),
				treeCompactionEntry("c2", "u3", firstKeptID, createdAt.Add(4*time.Second)),
			}

			// Act by restoring the repeated-compaction sequence.
			tree, err := NewTree(entries, mo.Some("c2"), nil)

			// Assert same-boundary updates and forward movement remain valid.
			require.NoError(t, err)
			require.Len(t, tree.ActiveBranch(), 5)
		})
	}
}

// TestTreeRestoreAllowsCompactionAfterInterruptedToolBatch verifies skipped unpersisted results use history projection.
func TestTreeRestoreAllowsCompactionAfterInterruptedToolBatch(t *testing.T) {
	t.Parallel()

	// Arrange an interrupted two-call batch with only its active call result persisted.
	createdAt := time.Unix(1, 0).UTC()
	arguments, err := model.NewToolCallArguments([]byte(`{}`))
	require.NoError(t, err)
	root := treeUserEntry("root", mo.None[string](), "root", createdAt)
	call := treeToolCallEntry("call", "root", "active", arguments, createdAt.Add(time.Second))
	response := call.Model.MustGet()
	response.Content = append(response.Content, model.Content{
		Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.Some(model.ToolCall{ID: "skipped", Name: "tool", Arguments: arguments}),
	})
	call.Model = mo.Some(response)
	result := treeBaseEntry("result", mo.Some("call"), createdAt.Add(2*time.Second))
	result.ToolResult = mo.Some(ToolResult{CallID: "active", ToolName: "tool", Contents: nil, IsError: true})
	later := treeUserEntry("later", mo.Some("result"), "later", createdAt.Add(3*time.Second))
	marker := treeCompactionEntry("compaction", "later", "later", createdAt.Add(4*time.Second))

	// Act by restoring compaction after the interrupted batch.
	tree, err := NewTree([]Entry{root, call, result, later, marker}, mo.Some("compaction"), nil)

	// Assert the omitted skipped result does not imply a permanently pending batch.
	require.NoError(t, err)
	require.Len(t, tree.ActiveBranch(), 5)
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

// treeToolCallEntry returns one model response that owns a finalized tool call.
func treeToolCallEntry(
	id string,
	parentID string,
	callID string,
	arguments model.ToolCallArguments,
	createdAt time.Time,
) Entry {
	entry := treeBaseEntry(id, mo.Some(parentID), createdAt)
	entry.Model = mo.Some(model.Response{
		Content: []model.Content{{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.Some(model.ToolCall{ID: callID, Name: "tool", Arguments: arguments}),
		}},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	})
	return entry
}

// treeCompactionEntry returns one stored compaction marker.
func treeCompactionEntry(id, parentID, firstKeptID string, createdAt time.Time) Entry {
	entry := treeBaseEntry(id, mo.Some(parentID), createdAt)
	entry.Compaction = mo.Some(CompactionEntry{
		Summary: "summary", FirstKeptEntryID: firstKeptID,
		Source: CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[EstimatedCost](), Details: mo.None[[]byte](),
	})
	return entry
}

// treeBaseEntry returns one empty tree entry for test payload construction.
func treeBaseEntry(id string, parentID mo.Option[string], createdAt time.Time) Entry {
	return Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[Information](), User: mo.None[UserMessage](), Model: mo.None[ModelResponse](),
		EstimatedCost: mo.None[EstimatedCost](), ToolResult: mo.None[ToolResult](),
		Extension: mo.None[ExtensionEnvelope](), ExtensionMessage: mo.None[ExtensionMessage](),
		BranchSummary: mo.None[BranchSummaryEntry](), Compaction: mo.None[CompactionEntry](),
	}
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
