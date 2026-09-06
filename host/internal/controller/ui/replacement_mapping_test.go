//go:build !integration

package ui

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapReplacementAndLabelCommandsPreservesTypedArguments verifies all public UI command variants.
func TestMapReplacementAndLabelCommandsPreservesTypedArguments(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		set            func(*uiv1.UIRequest)
		expectedKind   CommandKind
		expectedTarget mo.Option[string]
		expectedLabel  mo.Option[string]
	}{
		{name: "fork", set: func(response *uiv1.UIRequest) {
			response.SetForkSession(uiv1.ForkSessionCommand_builder{TargetEntryId: new("entry")}.Build())
		}, expectedKind: CommandForkSession, expectedTarget: mo.Some("entry"), expectedLabel: mo.None[string]()},
		{name: "clone", set: func(response *uiv1.UIRequest) {
			response.SetCloneSession(new(uiv1.CloneSessionCommand))
		}, expectedKind: CommandCloneSession, expectedTarget: mo.None[string](), expectedLabel: mo.None[string]()},
		{name: "clear label", set: func(response *uiv1.UIRequest) {
			response.SetSetEntryLabel(uiv1.SetEntryLabelCommand_builder{TargetEntryId: new("entry"), Label: new("")}.Build())
		}, expectedKind: CommandSetEntryLabel, expectedTarget: mo.Some("entry"), expectedLabel: mo.Some("")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one typed protobuf command.
			request := new(uiv1.UIRequest)
			test.set(request)

			// Act at the UI process boundary.
			command, err := mapCommand(operationResponse(request))

			// Assert optional values preserve presence and exact content.
			require.NoError(t, err)
			require.Equal(t, test.expectedKind, command.Kind)
			require.Equal(t, test.expectedTarget, command.TargetEntryID)
			require.Equal(t, test.expectedLabel, command.EntryLabel)
		})
	}
}
