package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestModelEmitsStopRetryAndQuitFromDocumentedKeys verifies the documented control bindings.
func TestModelEmitsStopRetryAndQuitFromDocumentedKeys(t *testing.T) {
	t.Parallel()

	// Arrange models in running, failed, and idle states with command capture.
	var commands []presentationdomain.Command
	emit := func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	}
	model := newTestModel(t, presentationdomain.AvailabilityRunning, emit)
	// Act by applying the documented stop, retry, and quit keys.
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'c',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityAuthenticationFailed),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
	}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'r',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	next, command := model.Update(tea.KeyPressMsg(tea.Key{
		Code:        'q',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = next.(Model)
	// Assert each key emits the documented command or quit message.
	require.NotNil(t, command)
	message := command()
	assert.IsType(t, emissionResultMsg{}, message)
	_, quit := model.Update(message)
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())

	assert.Equal(t, []presentationdomain.Command{
		{
			Kind:            presentationdomain.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind:            presentationdomain.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind:       presentationdomain.CommandQuit,
			Text:       mo.None[string](),
			ProviderID: mo.None[string](),

			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
	}, commands)
}
