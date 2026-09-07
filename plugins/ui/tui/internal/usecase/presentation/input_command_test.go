//go:build !integration

package presentation

import (
	"testing"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelEmitsStopRetryAndQuitFromDocumentedKeys verifies the documented control bindings.
func TestModelEmitsStopRetryAndQuitFromDocumentedKeys(t *testing.T) {
	t.Parallel()

	// Arrange models in running, failed, and idle states with command capture.
	var commands []Command
	emit := func(command Command) error {
		commands = append(commands, command)
		return nil
	}
	model := newTestModel(t, AvailabilityRunning, emit)
	model.foreground = "running"
	// Act by applying the documented stop, retry, and quit keys.
	model = executeCommand(t, model, inputcontroller.Key{
		Code: 'c',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 eventAvailability,
		Availability:         mo.Some(AvailabilityAuthenticationFailed),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
	}))
	model = executeCommand(t, model, inputcontroller.Key{
		Code: 'r',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})

	next, command := updateApplication(model, inputcontroller.Key{
		Code: 'q',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	model = next
	// Assert each key emits the documented command or quit message.
	require.NotNil(t, command)
	message := command.Execute()
	assert.IsType(t, inputcontroller.Result{}, message)
	quit := model.Complete(message)
	require.True(t, quit)
	assert.True(t, quit)

	assert.Equal(t, []Command{
		{
			Kind:            CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[TreeCommand](),
		},
		{
			Kind:            CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[TreeCommand](),
		},
		{
			Kind:       CommandQuit,
			Text:       mo.None[string](),
			ProviderID: mo.None[string](),

			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[TreeCommand](),
		},
	}, commands)
}
