//go:build !integration

package runtime

import (
	"testing"
	"time"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapTreeFramePreservesExtensionMessage verifies exact text and visibility reach the UI contract.
func TestMapTreeFramePreservesExtensionMessage(t *testing.T) {
	t.Parallel()

	// Arrange one complete tree frame with a hidden-client extension message.
	frame := runtimeTreeFrame(controllerui.FrameSessionTree)
	frame.SessionTree = mo.Some(controllerui.SessionTree{
		Entries: []controllerui.SessionTreeEntry{
			{
				ID:            "message",
				ParentID:      mo.Some("parent"),
				CreatedAt:     time.Unix(1, 0).UTC(),
				Label:         "",
				Kind:          controllerui.SessionTreeEntryExtensionMessage,
				User:          mo.None[model.Message](),
				Model:         mo.None[controllerui.ModelResponse](),
				ToolResult:    mo.None[agent.ToolResult](),
				Extension:     mo.None[controllerui.ExtensionEntry](),
				BranchSummary: mo.None[controllerui.BranchSummary](),
				ExtensionMessage: mo.Some(controllerui.ExtensionMessage{
					ExtensionID: "example",
					EntryType:   "note",
					Text:        "exact text",
					Visibility:  session.ClientVisibilityHidden,
				}),
			},
		}, ActiveLeafID: mo.Some("message"),
	})

	// Act by mapping the Host frame to protobuf.
	wire, err := mapFrame(frame)

	// Assert exact message data and visibility remain present.
	require.NoError(t, err)
	entry := wire.GetEvent().GetCompleted().GetSessionTree().GetTree().GetEntries()[0]
	require.Equal(t, "exact text", entry.GetExtensionMessage().GetText())
	require.Equal(t, uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN, entry.GetExtensionMessage().GetVisibility())
}

// TestMapTreeFramePreservesPublicState verifies parent, label, active leaf, and opaque extension metadata reach the UI
// contract.
func TestMapTreeFramePreservesPublicState(t *testing.T) {
	t.Parallel()

	// Arrange one complete public tree frame.
	frame := runtimeTreeFrame(controllerui.FrameSessionTree)
	frame.SessionTree = mo.Some(controllerui.SessionTree{
		Entries: []controllerui.SessionTreeEntry{{
			ID: "extension", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(), Label: "checkpoint",
			Kind: controllerui.SessionTreeEntryExtension,
			User: mo.None[model.Message](), Model: mo.None[controllerui.ModelResponse](),
			ToolResult:    mo.None[agent.ToolResult](),
			Extension:     mo.Some(controllerui.ExtensionEntry{ExtensionID: "example", EntryType: "state"}),
			BranchSummary: mo.None[controllerui.BranchSummary](),
		}}, ActiveLeafID: mo.Some("extension"),
	})

	// Act by mapping the Host frame to protobuf.
	wire, err := mapFrame(frame)

	// Assert the complete state is present and extension payload contains identifiers only.
	require.NoError(t, err)
	tree := wire.GetEvent().GetCompleted().GetSessionTree().GetTree()
	require.Equal(t, "extension", tree.GetActiveLeafId())
	require.Len(t, tree.GetEntries(), 1)
	require.Equal(t, uiv1.SessionTreeEntry_Extension_case, tree.GetEntries()[0].WhichEntry())
	require.Equal(t, "example", tree.GetEntries()[0].GetExtension().GetExtensionId())
}

// TestMapCommittedTreeNavigationPreservesExactInput verifies terminal UI results contain commit metadata and editable input.
func TestMapCommittedTreeNavigationPreservesExactInput(t *testing.T) {
	t.Parallel()

	// Arrange one committed navigation frame with exact metadata and next input.
	frame := runtimeTreeFrame(controllerui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(controllerui.TreeNavigationResult{
		Status: controllerui.TreeNavigationStatusCommitted,
		Committed: mo.Some(controllerui.TreeNavigationCommitted{
			DestinationID: mo.Some("destination"), ActiveLeafID: mo.Some("leaf"),
			CreatedSummary: mo.None[controllerui.SessionTreeEntry](), NextInput: mo.Some("exact input"),
		}),
		Issues: []controllerui.OperationIssue{
			{
				Code:        controllerui.OperationIssueObserverError,
				ExtensionID: "extension",
				HandlerID:   "observer",
				Message:     "safe message",
			},
		},
	})

	// Act by mapping the committed frame.
	wire, err := mapFrame(frame)

	// Assert committed status, metadata, and exact next input reach the contract.
	require.NoError(t, err)
	result := wire.GetEvent().GetCompleted().GetSessionTreeNavigation()
	require.Equal(t, uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED, result.GetStatus())
	require.Equal(t, "destination", result.GetDestinationId())
	require.Equal(t, "leaf", result.GetActiveLeafId())
	require.True(t, result.HasNextInput())
	require.Equal(t, "exact input", result.GetNextInput())
	require.Equal(t, uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_OBSERVER_ERROR, result.GetIssues()[0].GetCode())
	require.Equal(t, "extension", result.GetIssues()[0].GetExtensionId())
	require.Equal(t, "observer", result.GetIssues()[0].GetHandlerId())
	require.Equal(t, "safe message", result.GetIssues()[0].GetMessage())
}

// TestMapCanceledTreeNavigationOmitsSpeculativeState verifies canceled UI results contain status only.
func TestMapCanceledTreeNavigationOmitsSpeculativeState(t *testing.T) {
	t.Parallel()

	// Arrange one canceled frame without committed state.
	frame := runtimeTreeFrame(controllerui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(controllerui.TreeNavigationResult{
		Status:    controllerui.TreeNavigationStatusCanceled,
		Committed: mo.None[controllerui.TreeNavigationCommitted](), Issues: nil,
	})

	// Act by mapping the canceled frame.
	wire, err := mapFrame(frame)

	// Assert canceled status and omitted next input are emitted.
	require.NoError(t, err)
	result := wire.GetEvent().GetCompleted().GetSessionTreeNavigation()
	require.Equal(t, uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED, result.GetStatus())
	require.False(t, result.HasNextInput())
}

// TestMapTreeOptionalPresenceDistinguishesEmptyFromAbsent verifies optional UI strings retain explicit empty values.
func TestMapTreeOptionalPresenceDistinguishesEmptyFromAbsent(t *testing.T) {
	t.Parallel()

	// Arrange tree and navigation frames with explicit empty optional strings.
	tree := controllerui.SessionTree{
		Entries: []controllerui.SessionTreeEntry{{
			ID: "extension", ParentID: mo.Some(""), CreatedAt: time.Unix(1, 0).UTC(), Label: "",
			Kind: controllerui.SessionTreeEntryExtension, User: mo.None[model.Message](),
			Model: mo.None[controllerui.ModelResponse](), ToolResult: mo.None[agent.ToolResult](),
			Extension:     mo.Some(controllerui.ExtensionEntry{ExtensionID: "example", EntryType: "state"}),
			BranchSummary: mo.None[controllerui.BranchSummary](),
		}}, ActiveLeafID: mo.Some(""),
	}
	treeFrame := runtimeTreeFrame(controllerui.FrameSessionTree)
	treeFrame.SessionTree = mo.Some(tree)
	navigationFrame := runtimeTreeFrame(controllerui.FrameSessionTreeNavigation)
	navigationFrame.TreeNavigation = mo.Some(controllerui.TreeNavigationResult{
		Status: controllerui.TreeNavigationStatusCommitted,
		Committed: mo.Some(controllerui.TreeNavigationCommitted{
			DestinationID: mo.Some(""), ActiveLeafID: mo.Some(""),
			CreatedSummary: mo.None[controllerui.SessionTreeEntry](), NextInput: mo.Some(""),
		}),
		Issues: nil,
	})

	// Act by mapping explicit empty values and absent values through the public contract.
	treeWire, treeErr := mapFrame(treeFrame)
	navigationWire, navigationErr := mapFrame(navigationFrame)
	absentTree, absentTreeErr := mapSessionTree(controllerui.SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()})

	// Assert explicit empty values remain present, absent values remain absent, and fields use proto3 optional presence.
	require.NoError(t, treeErr)
	require.NoError(t, navigationErr)
	require.NoError(t, absentTreeErr)
	mappedTree := treeWire.GetEvent().GetCompleted().GetSessionTree().GetTree()
	require.True(t, mappedTree.HasActiveLeafId())
	require.Empty(t, mappedTree.GetActiveLeafId())
	require.True(t, mappedTree.GetEntries()[0].HasParentId())
	require.Empty(t, mappedTree.GetEntries()[0].GetParentId())
	mappedNavigation := navigationWire.GetEvent().GetCompleted().GetSessionTreeNavigation()
	require.True(t, mappedNavigation.HasNextInput())
	require.Empty(t, mappedNavigation.GetNextInput())
	require.False(t, absentTree.HasActiveLeafId())
}

// runtimeTreeFrame initializes all frame fields for one tree result.
func runtimeTreeFrame(kind controllerui.FrameKind) controllerui.Frame {
	return controllerui.Frame{
		NextInput: mo.None[string](),
		Kind:      kind,

		Lifecycle:        mo.None[controllerui.Lifecycle](),
		AuthorizationURL: mo.None[string](),

		ModelSelection:    mo.None[model.Selection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[controllerui.SessionTree](),
		TreeNavigation:    mo.None[controllerui.TreeNavigationResult](),
	}
}
