//go:build !integration

package plugin

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapSessionTreeRetainsExtensionMessageState verifies exact message data remains available outside the transcript.
func TestMapSessionTreeRetainsExtensionMessageState(t *testing.T) {
	t.Parallel()

	// Arrange one complete tree with a hidden-client extension message.
	entry := uiv1.SessionTreeEntry_builder{
		Id:          new("message"),
		ParentId:    new("parent"),
		CreatedTime: timestamppb.New(time.Unix(1, 0).UTC()),
		Label: new(
			"",
		),
		User:          nil,
		Model:         nil,
		ToolResult:    nil,
		Extension:     nil,
		BranchSummary: nil,
		ExtensionMessage: uiv1.ExtensionMessage_builder{
			ExtensionId: new("example"), EntryType: new("note"), Text: new("exact text"),
			Visibility: new(uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
		}.Build(),
	}.Build()

	// Act by mapping complete tree state to the TUI presentation domain.
	tree, err := mapSessionTree(uiv1.SessionTree_builder{
		Entries: []*uiv1.SessionTreeEntry{entry}, ActiveLeafId: new("message"),
	}.Build())

	// Assert exact metadata, text, visibility, and active leaf remain available.
	require.NoError(t, err)
	require.Equal(t, mo.Some("message"), tree.ActiveLeafID)
	require.Len(t, tree.Entries, 1)
	require.Equal(t, TreeEntryExtensionMessage, tree.Entries[0].Kind)
	require.Equal(t, "exact text", tree.Entries[0].ExtensionMessage.MustGet().Text)
	require.Equal(t, ClientVisibilityHidden, tree.Entries[0].ExtensionMessage.MustGet().Visibility)
}

// TestMapRequestDecodesTreeReplacementAndLabelFrames verifies every new Host frame is supported.
func TestMapRequestDecodesTreeReplacementAndLabelFrames(t *testing.T) {
	t.Parallel()

	createdAt := timestamppb.New(time.Unix(1, 0).UTC())
	tree := uiv1.SessionTree_builder{
		Entries: []*uiv1.SessionTreeEntry{
			uiv1.SessionTreeEntry_builder{
				Id: new("entry"), ParentId: nil, CreatedTime: createdAt, Label: new("checkpoint"), User: nil,
				Model: nil, ToolResult: nil,
				Extension:     uiv1.ExtensionEntry_builder{ExtensionId: new("audit"), EntryType: new("record")}.Build(),
				BranchSummary: nil, ExtensionMessage: nil,
			}.Build(),
		},
		ActiveLeafId: new("entry"),
	}.Build()
	session := uiv1.SessionChanged_builder{Info: testSessionInfo(), Entries: nil}.Build()

	for _, testCase := range []struct {
		name      string
		request   *uiv1.HostCompleted
		kind      TreeKind
		nextInput mo.Option[string]
	}{
		{
			name: "forked",
			//nolint:exhaustruct_v5 // The protobuf builder sets only the active SessionForked field.
			request: uiv1.HostCompleted_builder{
				SessionForked: uiv1.SessionForked_builder{Session: session, NextInput: new(" exact input ")}.Build(),
			}.Build(),
			kind: TreeForked, nextInput: mo.Some(" exact input "),
		},
		{
			name: "cloned",
			//nolint:exhaustruct_v5 // The protobuf builder sets only the active SessionCloned field.
			request: uiv1.HostCompleted_builder{
				SessionCloned: uiv1.SessionCloned_builder{Session: session}.Build(),
			}.Build(),
			kind: TreeCloned, nextInput: mo.None[string](),
		},
		{
			name: "label set",
			//nolint:exhaustruct_v5 // The protobuf builder sets only the active EntryLabelSet field.
			request: uiv1.HostCompleted_builder{
				EntryLabelSet: uiv1.EntryLabelSet_builder{Tree: tree}.Build(),
			}.Build(),
			kind: TreeLabelSet, nextInput: mo.None[string](),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapTreeRequest to verify every new Host frame is supported.

			// Act by decoding the typed Host frame.
			// Act by invoking mapTreeRequest to exercise every new Host frame is supported.
			event, _, err := mapTreeRequest(testCase.request)

			// Assert the frame maps to typed presentation state instead of an unknown frame.
			// Assert every new Host frame is supported.
			require.NoError(t, err)
			require.Equal(t, testCase.kind, event.Tree.Kind)
			mapped := event.Tree
			require.Equal(t, testCase.nextInput, mapped.NextInput)
			if testCase.kind == TreeLabelSet {
				mappedTree, treePresent := mapped.Tree.Get()
				require.True(t, treePresent)
				require.Equal(t, "checkpoint", mappedTree.Entries[0].Label)
				require.Equal(t, TreeEntryExtension, mappedTree.Entries[0].Kind)
				require.Equal(t, "audit record", mappedTree.Entries[0].Text)
			}
		})
	}
}
