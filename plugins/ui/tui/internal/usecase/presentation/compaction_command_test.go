//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestCompactSlashCommandPreservesOptionalInstructions verifies the standard command emits manual compaction.
func TestCompactSlashCommandPreservesOptionalInstructions(t *testing.T) {
	t.Parallel()
	// Arrange an idle presentation with exact command capture.
	var commands []Command
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})
	model.model.input = []rune("/compact preserve decisions")
	model.model.cursor = len(model.model.input)

	// Act by submitting the standard slash command.
	_ = executeCommand(t, model, testKey(inputcontroller.KeyEnter))

	// Assert instructions are sent through the typed manual compaction command.
	require.Len(t, commands, 1)
	require.Equal(t, CommandCompact, commands[0].Kind)
	require.Equal(t, mo.Some("preserve decisions"), commands[0].Text)
}
