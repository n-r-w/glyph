//go:build !integration

package plugin

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

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
				BranchSummary: nil,
			}.Build(),
		},
		ActiveLeafId: new("entry"),
	}.Build()
	session := uiv1.SessionChanged_builder{Info: testSessionInfo(), Entries: nil}.Build()

	for _, testCase := range []struct {
		name      string
		request   *uiv1.HostCompleted
		kind      presentation.EventKind
		nextInput mo.Option[string]
	}{
		{
			name: "forked",
			//nolint:exhaustruct_v5 // The protobuf builder sets only the active SessionForked field.
			request: uiv1.HostCompleted_builder{
				SessionForked: uiv1.SessionForked_builder{Session: session, NextInput: new(" exact input ")}.Build(),
			}.Build(),
			kind: presentation.EventSessionForked, nextInput: mo.Some(" exact input "),
		},
		{
			name: "cloned",
			//nolint:exhaustruct_v5 // The protobuf builder sets only the active SessionCloned field.
			request: uiv1.HostCompleted_builder{
				SessionCloned: uiv1.SessionCloned_builder{Session: session}.Build(),
			}.Build(),
			kind: presentation.EventSessionCloned, nextInput: mo.None[string](),
		},
		{
			name: "label set",
			//nolint:exhaustruct_v5 // The protobuf builder sets only the active EntryLabelSet field.
			request: uiv1.HostCompleted_builder{
				EntryLabelSet: uiv1.EntryLabelSet_builder{Tree: tree}.Build(),
			}.Build(),
			kind: presentation.EventEntryLabelSet, nextInput: mo.None[string](),
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
			require.Equal(t, testCase.kind, event.Kind)
			mapped, present := event.TreeEvent.Get()
			require.True(t, present)
			require.Equal(t, testCase.nextInput, mapped.NextInput)
			if testCase.kind == presentation.EventEntryLabelSet {
				mappedTree, treePresent := mapped.Tree.Get()
				require.True(t, treePresent)
				require.Equal(t, "checkpoint", mappedTree.Entries[0].Label)
				require.Equal(t, presentation.TreeEntryExtension, mappedTree.Entries[0].Kind)
				require.Equal(t, "audit record", mappedTree.Entries[0].Text)
			}
		})
	}
}

// TestMapCommandEncodesTreeOperations verifies typed tree commands use the public UI contract.
func TestMapCommandEncodesTreeOperations(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		command presentation.Command
		assert  func(*testing.T, *uiv1.UIRequest)
	}{
		{
			name: "get tree",
			command: treeMappingCommand(presentation.CommandGetSessionTree, presentation.TreeCommand{
				TargetEntryID: mo.None[string](), SummaryMode: presentation.SummaryModeUnspecified,
				CustomFocus: mo.None[string](), Label: mo.None[string](),
			}),
			assert: func(t *testing.T, response *uiv1.UIRequest) { require.NotNil(t, response.GetGetSessionTree()) },
		},
		{
			name: "navigate with custom focus",
			command: treeMappingCommand(presentation.CommandNavigateSessionTree, presentation.TreeCommand{
				TargetEntryID: mo.Some("target"), SummaryMode: presentation.SummaryModeCustomFocus,
				CustomFocus: mo.Some("focus"), Label: mo.None[string](),
			}),
			assert: func(t *testing.T, response *uiv1.UIRequest) {
				command := response.GetNavigateSessionTree()
				require.Equal(t, "target", command.GetTargetEntryId())
				require.Equal(t, uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT, command.GetSummaryMode())
				require.Equal(t, "focus", command.GetCustomFocus())
			},
		},
		{
			name: "fork",
			command: treeMappingCommand(presentation.CommandForkSession, presentation.TreeCommand{
				TargetEntryID: mo.Some("target"), SummaryMode: presentation.SummaryModeUnspecified,
				CustomFocus: mo.None[string](), Label: mo.None[string](),
			}),
			assert: func(t *testing.T, response *uiv1.UIRequest) {
				require.Equal(t, "target", response.GetForkSession().GetTargetEntryId())
			},
		},
		{
			name: "clone",
			command: treeMappingCommand(presentation.CommandCloneSession, presentation.TreeCommand{
				TargetEntryID: mo.None[string](), SummaryMode: presentation.SummaryModeUnspecified,
				CustomFocus: mo.None[string](), Label: mo.None[string](),
			}),
			assert: func(t *testing.T, response *uiv1.UIRequest) { require.NotNil(t, response.GetCloneSession()) },
		},
		{
			name: "set label",
			command: treeMappingCommand(presentation.CommandSetEntryLabel, presentation.TreeCommand{
				TargetEntryID: mo.Some("target"), SummaryMode: presentation.SummaryModeUnspecified,
				CustomFocus: mo.None[string](), Label: mo.Some(""),
			}),
			assert: func(t *testing.T, response *uiv1.UIRequest) {
				command := response.GetSetEntryLabel()
				require.True(t, command.HasLabel())
				require.Empty(t, command.GetLabel())
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapCommand to verify typed tree commands use the public UI contract.

			// Act by mapping one presentation command.
			// Act by invoking mapCommand to exercise typed tree commands use the public UI contract.
			response, err := mapCommand(testCase.command)

			// Assert the matching typed contract command is populated.
			// Assert typed tree commands use the public UI contract.
			require.NoError(t, err)
			testCase.assert(t, response)
		})
	}
}

// treeMappingCommand creates a complete tree command for mapping tests.
func treeMappingCommand(kind presentation.CommandKind, treeCommand presentation.TreeCommand) presentation.Command {
	return presentation.Command{
		Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[presentation.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TreeCommand: mo.Some(treeCommand),
	}
}
