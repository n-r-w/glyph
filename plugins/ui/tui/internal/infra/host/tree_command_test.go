//go:build !integration

package host

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentation "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

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
