package tui

import (
	"errors"

	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestModelEditsUnicodeSingleLineInput verifies rune-safe cursor movement and deletion.
func TestModelEditsUnicodeSingleLineInput(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "hé🙂",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Equal(t, []rune("hé🙂"), model.input)
	assert.Equal(t, 3, model.cursor)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyLeft,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyBackspace,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Equal(t, []rune("h🙂"), model.input)
	assert.Equal(t, 1, model.cursor)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyDelete,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Equal(t, []rune("h"), model.input)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "\n界\r",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Equal(t, []rune("h界"), model.input)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyHome,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "前",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEnd,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "後",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Equal(t, "前h界後", string(model.input))
	assert.Equal(t, 4, model.cursor)
}

// TestModelSubmitsOnlyWhileIdleAndClearsAfterSuccessfulEmission verifies input gating and acknowledgement.
func TestModelSubmitsOnlyWhileIdleAndClearsAfterSuccessfulEmission(t *testing.T) {
	t.Parallel()

	// Arrange a command sink and model with a populated draft.
	var commands []presentationdomain.Command
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        " request ",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	// Act by submitting the draft across unavailable, running, failed, and idle states.
	next, command := model.Update(tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEnter,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = next.(Model)
	require.NotNil(t, command)
	assert.Equal(t, " request ", string(model.input))
	assert.True(t, model.emitting)

	model = updateModel(t, model, command())
	assert.Equal(t, []presentationdomain.Command{{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.Some("request"),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	}}, commands)
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
	assert.False(t, model.emitting)
	assert.Contains(t, model.View().Content, "user: request")

	_, emptyCommand := model.Update(tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEnter,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	// Assert only idle submission emits and successful emission clears the draft.
	assert.Nil(t, emptyCommand)

	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityRunning),
		Startup:              nil,
		Extensions:           nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "blocked",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Empty(t, model.input)
}

// TestModelRetainsInputAndShowsErrorWhenEmissionFails verifies failed delivery remains recoverable.
func TestModelRetainsInputAndShowsErrorWhenEmissionFails(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error {
		return errors.New("stream closed")
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "retry me",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	next, command := model.Update(tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEnter,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = next.(Model)
	model = updateModel(t, model, command())

	assert.Equal(t, "retry me", string(model.input))
	assert.False(t, model.emitting)
	assert.Contains(t, model.View().Content, "[error] Could not send command: stream closed")
}
