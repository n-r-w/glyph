//go:build !integration

package presentation

import (
	"errors"
	"testing"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelEditsUnicodeSingleLineInput verifies rune-safe cursor movement and deletion.
func TestModelEditsUnicodeSingleLineInput(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, AvailabilityIdle, nil)
	model = updateModel(t, model, inputcontroller.Key{
		Text: "hé🙂",
		Mod:  0,
		Code: 0,
	})
	assert.Equal(t, []rune("hé🙂"), model.model.input)
	assert.Equal(t, 3, model.model.cursor)

	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyLeft,
		Text: "",
		Mod:  0,
	})
	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyBackspace,
		Text: "",
		Mod:  0,
	})
	assert.Equal(t, []rune("h🙂"), model.model.input)
	assert.Equal(t, 1, model.model.cursor)

	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyDelete,
		Text: "",
		Mod:  0,
	})
	assert.Equal(t, []rune("h"), model.model.input)
	model = updateModel(t, model, inputcontroller.Key{
		Text: "\n界\r",
		Mod:  0,
		Code: 0,
	})
	assert.Equal(t, []rune("h界"), model.model.input)

	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyHome,
		Text: "",
		Mod:  0,
	})
	model = updateModel(t, model, inputcontroller.Key{
		Text: "前",
		Mod:  0,
		Code: 0,
	})
	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyEnd,
		Text: "",
		Mod:  0,
	})
	model = updateModel(t, model, inputcontroller.Key{
		Text: "後",
		Mod:  0,
		Code: 0,
	})
	assert.Equal(t, "前h界後", string(model.model.input))
	assert.Equal(t, 4, model.model.cursor)
}

// TestModelSubmitsOnlyWhileIdleAndClearsAfterSuccessfulEmission verifies input gating and acknowledgement.
func TestModelSubmitsOnlyWhileIdleAndClearsAfterSuccessfulEmission(t *testing.T) {
	t.Parallel()

	// Arrange a command sink and model with a populated draft.
	var commands []Command
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})
	model = updateModel(t, model, inputcontroller.Key{
		Text: " request ",
		Mod:  0,
		Code: 0,
	})

	// Act by submitting the draft across unavailable, running, failed, and idle states.
	next, command := updateApplication(model, inputcontroller.Key{
		Code: inputcontroller.KeyEnter,
		Text: "",
		Mod:  0,
	})
	model = next
	require.NotNil(t, command)
	assert.Equal(t, " request ", string(model.model.input))
	assert.True(t, model.model.emitting)

	model = updateModel(t, model, command.Execute())
	assert.Equal(t, []Command{{
		Kind:            CommandSubmit,
		Text:            mo.Some("request"),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[TreeCommand](),
	}}, commands)
	assert.Empty(t, model.model.input)
	assert.Zero(t, model.model.cursor)
	assert.False(t, model.model.emitting)
	assert.Equal(t, mo.Some("request"), model.model.state.Transcript[len(model.model.state.Transcript)-1].Text)

	_, emptyCommand := updateApplication(model, inputcontroller.Key{
		Code: inputcontroller.KeyEnter,
		Text: "",
		Mod:  0,
	})
	// Assert only idle submission emits and successful emission clears the draft.
	assert.Nil(t, emptyCommand)

	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 eventAvailability,
		Availability:         mo.Some(AvailabilityRunning),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
	}))
	model = updateModel(t, model, inputcontroller.Key{
		Text: "blocked",
		Mod:  0,
		Code: 0,
	})
	assert.Empty(t, model.model.input)
}

// TestModelRetainsInputAndShowsErrorWhenEmissionFails verifies failed delivery remains recoverable.
func TestModelRetainsInputAndShowsErrorWhenEmissionFails(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, AvailabilityIdle, func(Command) error {
		return errors.New("stream closed")
	})
	model = updateModel(t, model, inputcontroller.Key{
		Text: "retry me",
		Mod:  0,
		Code: 0,
	})
	next, command := updateApplication(model, inputcontroller.Key{
		Code: inputcontroller.KeyEnter,
		Text: "",
		Mod:  0,
	})
	model = next
	model = updateModel(t, model, command.Execute())

	assert.Equal(t, "retry me", string(model.model.input))
	assert.False(t, model.model.emitting)
	assert.Equal(
		t,
		mo.Some("Could not send command: stream closed"),
		model.model.state.Transcript[len(model.model.state.Transcript)-1].Text,
	)
}
