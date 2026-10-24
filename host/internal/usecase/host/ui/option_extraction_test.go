//go:build !integration

package ui

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSessionOperationMissingValues verifies local extraction errors and subsequent valid label removal.
func TestSessionOperationMissingValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		kind      controllerui.CommandKind
		target    mo.Option[string]
		label     mo.Option[string]
		diagnosis string
	}{
		{name: "resume", kind: controllerui.CommandResumeSession, target: mo.None[string](), label: mo.None[string](), diagnosis: "session identifier is required"},
		{name: "name", kind: controllerui.CommandSetSessionName, target: mo.None[string](), label: mo.None[string](), diagnosis: "session name is required"},
		{name: "navigation", kind: controllerui.CommandNavigateSessionTree, target: mo.None[string](), label: mo.None[string](), diagnosis: "tree navigation request is invalid"},
		{name: "fork", kind: controllerui.CommandForkSession, target: mo.None[string](), label: mo.None[string](), diagnosis: "fork target is required"},
		{name: "label target", kind: controllerui.CommandSetEntryLabel, target: mo.None[string](), label: mo.Some(""), diagnosis: "entry label request is incomplete"},
		{name: "label value", kind: controllerui.CommandSetEntryLabel, target: mo.Some("target"), label: mo.None[string](), diagnosis: "entry label request is incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange strict dependencies that permit only the subsequent valid label deletion.
			controller := gomock.NewController(t)
			control := NewMockActiveSessions(controller)
			gate := NewMockGate(controller)
			service := replacementService(controller, control, gate)
			command := uiReplacementCommand(test.kind, test.target, test.label)
			control.EXPECT().SetLabel(gomock.Any(), "target", "").Return(session.Tree{}, nil)
			expectSessionMutationGate(gate, 1)

			// Act directly at execution because admission rejects the missing value.
			frame, err := service.runSessionOperation(t.Context(), command, operation.Reporter[controllerui.Frame]{})

			// Assert the missing value is diagnosed and a present empty label remains a valid admitted deletion.
			require.ErrorContains(t, err, test.diagnosis)
			require.Equal(t, controllerui.Frame{}, frame)
			frame, err = runPreparedCommand(
				t,
				service,
				uiReplacementCommand(controllerui.CommandSetEntryLabel, mo.Some("target"), mo.Some("")),
			)
			require.NoError(t, err)
			require.Equal(t, controllerui.FrameEntryLabelSet, frame.Kind)
		})
	}
}
