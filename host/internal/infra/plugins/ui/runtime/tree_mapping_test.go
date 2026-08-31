//go:build !integration

package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapTreeFramePreservesPublicState verifies parent, label, active leaf, and opaque extension metadata reach the UI
// contract.
func TestMapTreeFramePreservesPublicState(t *testing.T) {
	t.Parallel()

	// Arrange one complete public tree frame.
	frame := runtimeTreeFrame(domainui.FrameSessionTree)
	frame.SessionTree = mo.Some(domainui.SessionTree{
		Entries: []domainui.SessionTreeEntry{{
			ID: "extension", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(), Label: "checkpoint",
			Kind: domainui.SessionTreeEntryExtension,
			User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
			ToolResult:    mo.None[agent.ToolResult](),
			Extension:     mo.Some(domainui.ExtensionEntry{ExtensionID: "example", EntryType: "state"}),
			BranchSummary: mo.None[domainui.BranchSummary](),
		}}, ActiveLeafID: mo.Some("extension"),
	})

	// Act by mapping the Host frame to protobuf.
	wire, err := mapFrame(frame)

	// Assert the complete state is present and extension payload contains identifiers only.
	require.NoError(t, err)
	tree := wire.GetSessionTree().GetTree()
	require.Equal(t, "extension", tree.GetActiveLeafId())
	require.Len(t, tree.GetEntries(), 1)
	require.Equal(t, uipb.SessionTreeEntry_Extension_case, tree.GetEntries()[0].WhichEntry())
	require.Equal(t, "example", tree.GetEntries()[0].GetExtension().GetExtensionId())
}

// TestMapCommittedTreeNavigationPreservesExactInput verifies committed UI results contain tree and editable input.
func TestMapCommittedTreeNavigationPreservesExactInput(t *testing.T) {
	t.Parallel()

	// Arrange one committed navigation frame with an implicit-root tree and exact next input.
	frame := runtimeTreeFrame(domainui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(domainui.TreeNavigationResult{
		Status: domainui.TreeNavigationStatusCommitted,
		Committed: mo.Some(domainui.TreeNavigationCommitted{
			Tree:         domainui.SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()},
			ActiveBranch: nil, NextInput: mo.Some("exact input"),
		}),
		Issues: []domainui.OperationIssue{
			{
				Code:        domainui.OperationIssueObserverError,
				ExtensionID: "extension",
				HandlerID:   "observer",
				Message:     "safe message",
			},
		},
	})

	// Act by mapping the committed frame.
	wire, err := mapFrame(frame)

	// Assert committed status, tree presence, and exact next input reach the contract.
	require.NoError(t, err)
	result := wire.GetSessionTreeNavigation()
	require.Equal(t, uipb.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED, result.GetStatus())
	require.True(t, result.HasTree())
	require.True(t, result.HasNextInput())
	require.Equal(t, "exact input", result.GetNextInput())
	require.Equal(t, uipb.OperationIssueCode_OPERATION_ISSUE_CODE_OBSERVER_ERROR, result.GetIssues()[0].GetCode())
	require.Equal(t, "extension", result.GetIssues()[0].GetExtensionId())
	require.Equal(t, "observer", result.GetIssues()[0].GetHandlerId())
	require.Equal(t, "safe message", result.GetIssues()[0].GetMessage())
}

// TestMapCanceledTreeNavigationOmitsSpeculativeState verifies canceled UI results contain status only.
func TestMapCanceledTreeNavigationOmitsSpeculativeState(t *testing.T) {
	t.Parallel()

	// Arrange one canceled frame without committed state.
	frame := runtimeTreeFrame(domainui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(domainui.TreeNavigationResult{
		Status:    domainui.TreeNavigationStatusCanceled,
		Committed: mo.None[domainui.TreeNavigationCommitted](), Issues: nil,
	})

	// Act by mapping the canceled frame.
	wire, err := mapFrame(frame)

	// Assert no tree, active transcript, or next input is emitted.
	require.NoError(t, err)
	result := wire.GetSessionTreeNavigation()
	require.Equal(t, uipb.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED, result.GetStatus())
	require.False(t, result.HasTree())
	require.Empty(t, result.GetActiveBranch())
	require.False(t, result.HasNextInput())
}

// TestMapTreeOptionalPresenceDistinguishesEmptyFromAbsent verifies optional UI strings retain explicit empty values.
func TestMapTreeOptionalPresenceDistinguishesEmptyFromAbsent(t *testing.T) {
	t.Parallel()

	// Arrange tree and navigation frames with explicit empty optional strings.
	tree := domainui.SessionTree{
		Entries: []domainui.SessionTreeEntry{{
			ID: "extension", ParentID: mo.Some(""), CreatedAt: time.Unix(1, 0).UTC(), Label: "",
			Kind: domainui.SessionTreeEntryExtension, User: mo.None[model.Message](),
			Model: mo.None[domainui.ModelResponse](), ToolResult: mo.None[agent.ToolResult](),
			Extension:     mo.Some(domainui.ExtensionEntry{ExtensionID: "example", EntryType: "state"}),
			BranchSummary: mo.None[domainui.BranchSummary](),
		}}, ActiveLeafID: mo.Some(""),
	}
	treeFrame := runtimeTreeFrame(domainui.FrameSessionTree)
	treeFrame.SessionTree = mo.Some(tree)
	navigationFrame := runtimeTreeFrame(domainui.FrameSessionTreeNavigation)
	navigationFrame.TreeNavigation = mo.Some(domainui.TreeNavigationResult{
		Status: domainui.TreeNavigationStatusCommitted,
		Committed: mo.Some(domainui.TreeNavigationCommitted{
			Tree: tree, ActiveBranch: nil, NextInput: mo.Some(""),
		}),
		Issues: nil,
	})

	// Act by mapping explicit empty values and absent values through the public contract.
	treeWire, treeErr := mapFrame(treeFrame)
	navigationWire, navigationErr := mapFrame(navigationFrame)
	absentTree, absentTreeErr := mapSessionTree(domainui.SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()})

	// Assert explicit empty values remain present, absent values remain absent, and fields use proto3 optional presence.
	require.NoError(t, treeErr)
	require.NoError(t, navigationErr)
	require.NoError(t, absentTreeErr)
	mappedTree := treeWire.GetSessionTree().GetTree()
	require.True(t, mappedTree.HasActiveLeafId())
	require.Empty(t, mappedTree.GetActiveLeafId())
	require.True(t, mappedTree.GetEntries()[0].HasParentId())
	require.Empty(t, mappedTree.GetEntries()[0].GetParentId())
	mappedNavigation := navigationWire.GetSessionTreeNavigation()
	require.True(t, mappedNavigation.HasNextInput())
	require.Empty(t, mappedNavigation.GetNextInput())
	require.False(t, absentTree.HasActiveLeafId())
	require.True(
		t,
		mappedNavigation.ProtoReflect().
			Descriptor().
			Fields().
			ByName(protoreflect.Name("next_input")).
			HasOptionalKeyword(),
	)
	require.True(
		t,
		mappedTree.ProtoReflect().
			Descriptor().
			Fields().
			ByName(protoreflect.Name("active_leaf_id")).
			HasOptionalKeyword(),
	)
	require.True(
		t,
		mappedTree.GetEntries()[0].ProtoReflect().
			Descriptor().
			Fields().
			ByName(protoreflect.Name("parent_id")).
			HasOptionalKeyword(),
	)
}

// runtimeTreeFrame initializes all frame fields for one tree result.
func runtimeTreeFrame(kind domainui.FrameKind) domainui.Frame {
	return domainui.Frame{
		Kind: kind, Initialization: mo.None[domainui.Initialization](), Lifecycle: mo.None[domainui.Lifecycle](),
		AuthorizationURL: mo.None[string](), Text: mo.None[string](), RetryAuthentication: mo.None[bool](),
		ModelSelection: mo.None[domainui.ModelSelection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](),
		SessionTree: mo.None[domainui.SessionTree](), TreeNavigation: mo.None[domainui.TreeNavigationResult](),
		TreeFailure: mo.None[domainui.TreeFailure](),
	}
}
