package programmatic

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapSessionTreeResponsePreservesPublicTreeState verifies the wire result contains parent, label, active leaf, and opaque extension metadata.
func TestMapSessionTreeResponsePreservesPublicTreeState(t *testing.T) {
	t.Parallel()

	// Arrange one complete internal tree result with no extension payload bytes.
	tree := SessionTree{
		Entries: []SessionTreeEntry{{
			ID: "extension", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(), Label: "checkpoint",
			Kind: SessionTreeEntryExtension, User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
			Extension:     mo.Some(ExtensionEntry{ExtensionID: "example", EntryType: "state"}),
			BranchSummary: mo.None[BranchSummary](),
		}},
		ActiveLeafID: mo.Some("extension"),
	}
	response := treeControllerResponse("tree", ResponseSessionTree)
	response.SessionTree = mo.Some(tree)

	// Act by mapping the controller response to the public protobuf contract.
	wire, err := mapResponse(response)

	// Assert the complete public state is present and extension data is limited to identifiers.
	require.NoError(t, err)
	result := wire.GetCommandResponse().GetSessionTree()
	require.Equal(t, "extension", result.GetTree().GetActiveLeafId())
	require.True(t, result.GetTree().HasActiveLeafId())
	require.Len(t, result.GetTree().GetEntries(), 1)
	entry := result.GetTree().GetEntries()[0]
	require.Equal(t, "parent", entry.GetParentId())
	require.Equal(t, "checkpoint", entry.GetLabel())
	require.Equal(t, programmaticv1.SessionTreeEntry_Extension_case, entry.WhichEntry())
	require.Equal(t, "example", entry.GetExtension().GetExtensionId())
	require.Equal(t, "state", entry.GetExtension().GetEntryType())
}

// TestMapCommittedNavigationPreservesExactInput verifies committed wire state contains tree and editable input.
func TestMapCommittedNavigationPreservesExactInput(t *testing.T) {
	t.Parallel()

	// Arrange one committed navigation result with an implicit-root tree and exact next input.
	response := treeControllerResponse("commit", ResponseSessionTreeNavigation)
	response.TreeNavigation = mo.Some(TreeNavigationResult{
		Status: TreeNavigationStatusCommitted,
		Committed: mo.Some(TreeNavigationCommitted{
			Tree:         SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()},
			ActiveBranch: nil, NextInput: mo.Some("exact input"),
		}),
		Issues: []OperationIssue{{
			Code: OperationIssueHandlerError, ExtensionID: "extension", HandlerID: "handler", Message: "safe message",
		}},
	})

	// Act by mapping the committed result.
	wire, err := mapResponse(response)

	// Assert committed status, tree presence, and exact next input reach the contract.
	require.NoError(t, err)
	result := wire.GetCommandResponse().GetSessionTreeNavigation()
	require.Equal(t, programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED, result.GetStatus())
	require.True(t, result.HasTree())
	require.True(t, result.HasNextInput())
	require.Equal(t, "exact input", result.GetNextInput())
	require.Equal(t, programmaticv1.OperationIssueCode_OPERATION_ISSUE_CODE_HANDLER_ERROR, result.GetIssues()[0].GetCode())
	require.Equal(t, "extension", result.GetIssues()[0].GetExtensionId())
	require.Equal(t, "handler", result.GetIssues()[0].GetHandlerId())
	require.Equal(t, "safe message", result.GetIssues()[0].GetMessage())
}

// TestMapCanceledNavigationOmitsSpeculativeState verifies cancellation has status only.
func TestMapCanceledNavigationOmitsSpeculativeState(t *testing.T) {
	t.Parallel()

	// Arrange one canceled navigation result without committed state.
	response := treeControllerResponse("cancel", ResponseSessionTreeNavigation)
	response.TreeNavigation = mo.Some(TreeNavigationResult{
		Status: TreeNavigationStatusCanceled, Committed: mo.None[TreeNavigationCommitted](), Issues: nil,
	})

	// Act by mapping the canceled result.
	wire, err := mapResponse(response)

	// Assert no tree, transcript, or next input is emitted.
	require.NoError(t, err)
	result := wire.GetCommandResponse().GetSessionTreeNavigation()
	require.Equal(t, programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED, result.GetStatus())
	require.False(t, result.HasTree())
	require.Empty(t, result.GetActiveBranch())
	require.False(t, result.HasNextInput())
}

// TestMapTreeOptionalPresenceDistinguishesEmptyFromAbsent verifies optional tree strings retain explicit empty values.
func TestMapTreeOptionalPresenceDistinguishesEmptyFromAbsent(t *testing.T) {
	t.Parallel()

	// Arrange tree and navigation values with explicit empty optional strings.
	tree := SessionTree{
		Entries: []SessionTreeEntry{{
			ID: "extension", ParentID: mo.Some(""), CreatedAt: time.Unix(1, 0).UTC(), Label: "",
			Kind: SessionTreeEntryExtension, User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
			Extension:     mo.Some(ExtensionEntry{ExtensionID: "example", EntryType: "state"}),
			BranchSummary: mo.None[BranchSummary](),
		}}, ActiveLeafID: mo.Some(""),
	}
	treeResponse := treeControllerResponse("tree-empty", ResponseSessionTree)
	treeResponse.SessionTree = mo.Some(tree)
	navigationResponse := treeControllerResponse("navigation-empty", ResponseSessionTreeNavigation)
	navigationResponse.TreeNavigation = mo.Some(TreeNavigationResult{
		Status: TreeNavigationStatusCommitted,
		Committed: mo.Some(TreeNavigationCommitted{
			Tree: tree, ActiveBranch: nil, NextInput: mo.Some(""),
		}),
		Issues: nil,
	})

	// Act by mapping explicit empty values and absent values through the public contract.
	treeWire, treeErr := mapResponse(treeResponse)
	navigationWire, navigationErr := mapResponse(navigationResponse)
	absentTree, absentTreeErr := mapSessionTree(SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()})

	// Assert explicit empty values remain present, absent values remain absent, and fields use proto3 optional presence.
	require.NoError(t, treeErr)
	require.NoError(t, navigationErr)
	require.NoError(t, absentTreeErr)
	mappedTree := treeWire.GetCommandResponse().GetSessionTree().GetTree()
	require.True(t, mappedTree.HasActiveLeafId())
	require.Empty(t, mappedTree.GetActiveLeafId())
	require.True(t, mappedTree.GetEntries()[0].HasParentId())
	require.Empty(t, mappedTree.GetEntries()[0].GetParentId())
	mappedNavigation := navigationWire.GetCommandResponse().GetSessionTreeNavigation()
	require.True(t, mappedNavigation.HasNextInput())
	require.Empty(t, mappedNavigation.GetNextInput())
	require.False(t, absentTree.HasActiveLeafId())
	require.True(t, mappedNavigation.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("next_input")).HasOptionalKeyword())
	require.True(t, mappedTree.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("active_leaf_id")).HasOptionalKeyword())
	require.True(t, mappedTree.GetEntries()[0].ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("parent_id")).HasOptionalKeyword())
}

// treeControllerResponse creates one fully initialized tree response.
func treeControllerResponse(correlationID string, kind ResponseKind) Response {
	return Response{
		CorrelationID: correlationID, Kind: kind, State: mo.None[RunStateResult](), Messages: nil,
		Models: mo.None[ModelsResult](), Selection: mo.None[model.Selection](),
		SessionInfo: mo.None[session.Info](), Sessions: nil, SessionEntries: nil,
		SessionStatistics: mo.None[session.Statistics](), SessionTree: mo.None[SessionTree](),
		TreeNavigation: mo.None[TreeNavigationResult](), Rejection: mo.None[Rejection](), Replacement: mo.None[SessionReplacement](),
	}
}
