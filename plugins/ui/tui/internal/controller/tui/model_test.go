package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
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
		Text:            "request",
		ProviderID:      "",
		ModelID:         "",
		ReasoningChoice: 0,
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
	assert.Nil(t, emptyCommand)

	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         presentationdomain.AvailabilityRunning,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
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

// TestModelSelectionShortcutsRespectAuthenticationAvailability verifies authentication gates only selection.
func TestModelSelectionShortcutsRespectAuthenticationAvailability(t *testing.T) {
	t.Parallel()

	keys := []tea.Key{
		{
			Code:        'l',
			Mod:         tea.ModCtrl,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
		{
			Code:        'p',
			Mod:         tea.ModCtrl,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
		{
			Code:        'p',
			Mod:         tea.ModShift | tea.ModCtrl,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
		{
			Code:        tea.KeyTab,
			Mod:         tea.ModShift,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
	}
	for _, availability := range []presentationdomain.Availability{
		presentationdomain.AvailabilityChecking,
		presentationdomain.AvailabilityAuthenticating,
	} {
		for _, key := range keys {
			model := newSelectionTestModel(t, availability, nil)
			next, command := model.Update(tea.KeyPressMsg(key))
			updated := next.(Model)
			assert.Nil(t, command)
			assert.False(t, updated.selectorOpen)
		}
	}
	for _, availability := range []presentationdomain.Availability{
		presentationdomain.AvailabilityIdle,
		presentationdomain.AvailabilityRunning,
		presentationdomain.AvailabilityAuthenticationFailed,
	} {
		for _, key := range keys {
			model := newSelectionTestModel(t, availability, nil)
			next, command := model.Update(tea.KeyPressMsg(key))
			updated := next.(Model)
			if key.Code == 'l' {
				assert.True(t, updated.selectorOpen)
				assert.Nil(t, command)
			} else {
				require.NotNil(t, command)
			}
		}
	}
}

// TestModelSingleSelectionCyclesEmitNothing verifies redundant selection commands are suppressed.
func TestModelSingleSelectionCyclesEmitNothing(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	for _, key := range []tea.Key{
		{
			Code:        'p',
			Mod:         tea.ModCtrl,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
		{
			Code:        'p',
			Mod:         tea.ModShift | tea.ModCtrl,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
		{
			Code:        tea.KeyTab,
			Mod:         tea.ModShift,
			Text:        "",
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		},
	} {
		model := NewModel(presentationdomain.Event{
			Kind:         presentationdomain.EventInitialization,
			Availability: presentationdomain.AvailabilityIdle,
			Models: []presentationdomain.ConfiguredModel{{
				ProviderID: "openai-codex",
				ModelID:    "gpt",
				Reasoning:  testReasoning(presentationdomain.ReasoningChoiceHigh),
			}},
			ModelSelection: presentationdomain.ModelSelection{
				ProviderID:      "openai-codex",
				ModelID:         "gpt",
				ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
			},
			Startup:              nil,
			Extensions:           nil,
			Position:             0,
			ModelContentKind:     0,
			ModelResponseContent: nil,
			ToolCallID:           "",
			ToolName:             "",
			Status:               "",
			Stream:               0,
			Text:                 "",
			ToolResultContents:   nil,
			ErrorText:            "",
			ExitCode:             0,
			Failure:              false,
			ToolCall:             presentationdomain.ToolCallState{},
		}, service.Apply, func(presentationdomain.Command) error {
			t.Fatal("redundant selection command emitted")
			return nil
		})

		_, command := model.Update(tea.KeyPressMsg(key))
		assert.Nil(t, command)
	}
}

// TestModelFixedReasoningHidesSelectionAndKeepsDisplayState verifies fixed-choice selection and local display state.
func TestModelFixedReasoningHidesSelectionAndKeepsDisplayState(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind:         presentationdomain.EventInitialization,
		Availability: presentationdomain.AvailabilityIdle,
		Models: []presentationdomain.ConfiguredModel{{
			ProviderID: "ollama",
			ModelID:    "ornith",
			Reasoning:  testReasoning(presentationdomain.ReasoningChoiceOn),
		}},
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		},
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
	}, service.Apply, func(presentationdomain.Command) error {
		t.Fatal("fixed reasoning selection command emitted")
		return nil
	})

	assert.NotContains(t, model.View().Content, "Shift+Tab reasoning")
	next, command := model.Update(tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyTab,
		Mod:         tea.ModShift,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = next.(Model)
	assert.Nil(t, command)
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelSelectionChanged,
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
	})
	assert.False(t, model.reasoningExpanded)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        't',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelSelectionChanged,
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
	})
	assert.True(t, model.reasoningExpanded)
}

// TestModelSelectorConfirmsAndCancelsWithoutChangingDraftOrTranscript verifies modal behavior.
func TestModelSelectorConfirmsAndCancelsWithoutChangingDraftOrTranscript(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	model := newSelectionTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model.input = []rune("draft")
	model.cursor = len(model.input)
	originalTranscript := slices.Clone(model.state.Transcript)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'l',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.True(t, model.selectorOpen)
	assert.Contains(t, model.View().Content, "openai-codex / gpt")
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyDown,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEnter,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.False(t, model.selectorOpen)
	assert.Equal(t, []presentationdomain.Command{{
		Kind:            presentationdomain.CommandSelectModel,
		ProviderID:      "openrouter",
		ModelID:         "sonnet",
		Text:            "",
		ReasoningChoice: 0,
	}}, commands)
	assert.Equal(t, "draft", string(model.input))
	assert.Equal(t, originalTranscript, model.state.Transcript)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'l',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEscape,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.False(t, model.selectorOpen)
	assert.Len(t, commands, 1)
	assert.Equal(t, "draft", string(model.input))
	assert.Equal(t, originalTranscript, model.state.Transcript)
}

// TestModelSelectorFitsTerminalAndKeepsEveryRowReachable verifies constrained selector rendering.
func TestModelSelectorFitsTerminalAndKeepsEveryRowReachable(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	models := make([]presentationdomain.ConfiguredModel, 8)
	for index := range models {
		models[index] = presentationdomain.ConfiguredModel{
			ProviderID: "provider",
			ModelID:    fmt.Sprintf("model-%d", index),
			Reasoning:  testReasoning(presentationdomain.ReasoningChoiceHigh),
		}
	}
	model := NewModel(presentationdomain.Event{
		Kind:         presentationdomain.EventInitialization,
		Availability: presentationdomain.AvailabilityIdle,
		Models:       models,
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID:      "provider",
			ModelID:         "model-0",
			ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
		},
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
	}, service.Apply, nil)
	model.height = 10
	model.input = []rune("draft")
	model.cursor = len(model.input)
	model.state.Transcript = []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineModel,
			Text:               "first",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		{
			Kind:               presentationdomain.LineModel,
			Text:               "second",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
	}
	originalTranscript := slices.Clone(model.state.Transcript)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'l',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	view := model.View().Content
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), model.height)
	assert.Contains(t, view, "> provider / model-0")
	assert.Contains(t, view, "Up/Down navigate | Enter confirm | Escape cancel")
	assert.NotContains(t, view, "provider / model-7")

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyUp,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Contains(t, model.View().Content, "> provider / model-7")
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyDown,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Contains(t, model.View().Content, "> provider / model-0")
	for range 7 {
		model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
			Code:        tea.KeyDown,
			Text:        "",
			Mod:         0,
			ShiftedCode: 0,
			BaseCode:    0,
			IsRepeat:    false,
		}))
	}
	view = model.View().Content
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), model.height)
	assert.Contains(t, view, "> provider / model-7")
	assert.Equal(t, "draft", string(model.input))
	assert.Equal(t, originalTranscript, model.state.Transcript)
}

// TestTypedModelCommandIsConsumedWhenOpeningSelector verifies the command does not enter transcript.
func TestTypedModelCommandIsConsumedWhenOpeningSelector(t *testing.T) {
	t.Parallel()

	model := newSelectionTestModel(t, presentationdomain.AvailabilityIdle, nil)
	originalTranscript := slices.Clone(model.state.Transcript)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "/model",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyEnter,
		Text:        "",
		Mod:         0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	assert.True(t, model.selectorOpen)
	assert.Empty(t, model.input)
	assert.Equal(t, originalTranscript, model.state.Transcript)
}

// TestModelSelectionCyclingWorksDuringRun verifies modifier data and configured wrap order.
func TestModelSelectionCyclingWorksDuringRun(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	model := newSelectionTestModel(t, presentationdomain.AvailabilityRunning, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'p',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'p',
		Mod:         tea.ModShift | tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        tea.KeyTab,
		Mod:         tea.ModShift,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	assert.Equal(t, []presentationdomain.Command{
		{
			Kind:            presentationdomain.CommandSelectModel,
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			Text:            "",
			ReasoningChoice: 0,
		},
		{
			Kind:            presentationdomain.CommandSelectModel,
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			Text:            "",
			ReasoningChoice: 0,
		},
		{
			Kind:            presentationdomain.CommandSelectReasoningChoice,
			ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
		},
	}, commands)
	assert.Equal(t, presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceLow,
	}, model.state.ModelSelection)
	assert.Contains(t, model.View().Content, "openai-codex / gpt / low")
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventError,
		Text:                 "selection failed",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	assert.Contains(t, model.View().Content, "openai-codex / gpt / low")
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelSelectionChanged,
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID:      "openai-codex",
			ModelID:         "gpt",
			ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
	})
	assert.Contains(t, model.View().Content, "openai-codex / gpt / high")
}

// TestModelEmitsStopRetryAndQuitFromDocumentedKeys verifies the documented control bindings.
func TestModelEmitsStopRetryAndQuitFromDocumentedKeys(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	emit := func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	}
	model := newTestModel(t, presentationdomain.AvailabilityRunning, emit)
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'c',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         presentationdomain.AvailabilityAuthenticationFailed,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
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
	require.NotNil(t, command)
	message := command()
	assert.IsType(t, emissionResultMsg{}, message)
	_, quit := model.Update(message)
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())

	assert.Equal(t, []presentationdomain.Command{
		{
			Kind:            presentationdomain.CommandStop,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
			ReasoningChoice: 0,
		},
		{
			Kind:            presentationdomain.CommandRetryAuthentication,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
			ReasoningChoice: 0,
		},
		{
			Kind:       presentationdomain.CommandQuit,
			Text:       "",
			ProviderID: "",

			ModelID:         "",
			ReasoningChoice: 0,
		},
	}, commands)
}

// TestModelRendersWarningAndExtensionIdentityPath verifies startup warning and path visibility.
func TestModelRendersWarningAndExtensionIdentityPath(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind: presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
			{
				Kind:               presentationdomain.LineWarning,
				Text:               "excluded UI optional at /plugins/ui/optional",
				ToolName:           "",
				Status:             "",
				ToolResultContents: nil,
			},
			{
				Kind:               presentationdomain.LineInformation,
				Text:               "UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read",
				ToolName:           "",
				Status:             "",
				ToolResultContents: nil,
			},
		},
		Extensions: []presentationdomain.Extension{{
			ID:    "glyph-tools",
			Path:  "/plugins/extension/glyph-tools",
			Tools: []string{"read"},
		}},
		Availability:         presentationdomain.AvailabilityIdle,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	}, service.Apply, func(presentationdomain.Command) error { return nil })

	view := model.View().Content
	assert.Contains(t, view, "[warning] excluded UI optional at /plugins/ui/optional")
	assert.Contains(t, view, "[info] UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read")
	assert.Equal(t, 1, strings.Count(view, "glyph-tools at /plugins/extension/glyph-tools"))
}

// TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize verifies complete view composition.
func TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind: presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{{
			Kind:               presentationdomain.LineInformation,
			Text:               "Glyph session initialized.",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		}},
		Availability:         presentationdomain.AvailabilityIdle,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	}, service.Apply, func(presentationdomain.Command) error { return nil })
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventInformation,
		Text:                 "Ready.",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		Text:                 "Working",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventAuthorization,
		Text:                 "https://example.test/oauth",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  100,
		Height: 40,
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "hello",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	view := model.View()
	assert.True(t, view.AltScreen)
	assert.Contains(t, view.Content, "Glyph session initialized.")
	assert.Contains(t, view.Content, "[info] Ready.")
	assert.Contains(t, view.Content, "assistant: Working")
	assert.Contains(t, view.Content, "Authorization: https://example.test/oauth")
	assert.Contains(t, view.Content, "Terminal: 100x40")
	assert.Contains(t, view.Content, "Request: hello|")
	assert.Contains(t, view.Content, "Ctrl+P next model | Shift+Ctrl+P previous model | Shift+Tab reasoning")
}

// TestModelEndDoesNotRenderDuplicateTextFromDifferentStreamPosition verifies terminal model replacement.
func TestModelEndDoesNotRenderDuplicateTextFromDifferentStreamPosition(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             0,
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		Text:                 "complete answer",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind:     presentationdomain.EventModelEnd,
		Position: 0,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "complete answer",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})

	assert.Empty(t, model.state.ActiveModel)
	assert.Equal(t, 1, strings.Count(model.View().Content, "complete answer"))
}

func TestModelRendersProvisionalToolCallNameFieldsAndPrefix(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, func(presentationdomain.Command) error { return nil })
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallPreview,
		ToolCall: presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: true,
			Fields: []presentationdomain.ToolCallField{
				{
					Name:     "path",
					Value:    "file.txt",
					Complete: true,
					Prefix:   "",
				},
				{
					Name:     "query",
					Prefix:   "hel",
					Value:    nil,
					Complete: false,
				},
			},
			Arguments: nil,
		},
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})

	view := model.View().Content
	assert.Contains(t, view, "[tool:call] read (provisional)")
	assert.Contains(t, view, `path="file.txt"`)
	assert.Contains(t, view, "query=hel")
	assert.NotContains(t, view, `{"path"`)
}

// TestModelReasoningUsesOneLocalCollapsedToggle verifies ordered markers, one shared toggle, and wrapped expansion.
func TestModelReasoningUsesOneLocalCollapsedToggle(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model.state.Transcript = append(model.state.Transcript,
		presentationdomain.Line{
			Kind:               presentationdomain.LineReasoning,
			Text:               "first reasoning block",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		presentationdomain.Line{
			Kind:               presentationdomain.LineModel,
			Text:               "between blocks",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
		presentationdomain.Line{
			Kind:               presentationdomain.LineReasoning,
			Text:               "second reasoning block",
			ToolName:           "",
			Status:             "",
			ToolResultContents: nil,
		},
	)
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  12,
		Height: 0,
	})
	view := model.View().Content
	assert.Contains(t, view, "Ctrl+T reasoning display")
	assert.NotContains(t, view, "Ctrl+O reasoning display")

	collapsed := strings.Join(model.visibleBodyLines(0), "\n")
	assert.Equal(t, 2, strings.Count(collapsed, "[collapsed]"))
	assert.NotContains(t, collapsed, "first reasoning")
	firstMarker := strings.Index(collapsed, "[collapsed]")
	between := strings.Index(collapsed, "between")
	secondMarker := strings.LastIndex(collapsed, "[collapsed]")
	assert.Less(t, firstMarker, between)
	assert.Less(t, between, secondMarker)
	assert.Len(t, model.state.Transcript, 3)

	model.emitting = true
	model.selectorOpen = true
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'o',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.False(t, model.reasoningExpanded)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        't',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.True(t, model.reasoningExpanded)
	assert.True(t, model.emitting)
	assert.True(t, model.selectorOpen)
	expandedLines := model.visibleBodyLines(0)
	expanded := strings.Join(expandedLines, "\n")
	assert.Contains(t, expanded, "first")
	assert.Contains(t, expanded, "second")
	for _, line := range expandedLines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 12)
	}
}

// TestModelWrapsCompletedUnicodeContent verifies readable wrapping, display width, and embedded line boundaries.
func TestModelWrapsCompletedUnicodeContent(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "readable words wrap cleanly\n你好 世界",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		Position:           0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: 0,
	})

	lines := model.visibleBodyLines(0)
	assert.Equal(t, []string{"assistant:", "readable words", "wrap cleanly", "你好 世界"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelWrapsActiveContent verifies word wrapping and long-token splitting for active streaming text.
func TestModelWrapsActiveContent(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		Text:                 "active words and supercalifragilistic",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: 0,
	})

	lines := model.visibleBodyLines(0)
	assert.Equal(t, []string{"assistant:", "active words and", "supercalifragili", "stic"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelClipsAfterWrapping verifies that the height budget selects wrapped visual lines.
func TestModelClipsAfterWrapping(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelDelta,
		Position:             1,
		Text:                 "active words and supercalifragilistic",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: fixedViewLineCount + 2,
	})
	assert.Equal(t, []string{"supercalifragili", "stic"}, model.visibleBodyLines(0))

	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  0,
		Height: 0,
	})
	assert.Equal(t, []string{"assistant: active words and supercalifragilistic"}, model.visibleBodyLines(0))
}

// TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight verifies viewport truncation.
func TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	for _, text := range []string{"oldest", "older", "middle", "newer", "latest"} {
		model = updateModel(t, model, presentationdomain.Event{
			Kind:                 presentationdomain.EventInformation,
			Text:                 text,
			Startup:              nil,
			Extensions:           nil,
			Availability:         0,
			Position:             0,
			ModelContentKind:     0,
			ModelResponseContent: nil,
			ToolCallID:           "",
			ToolName:             "",
			Status:               "",
			Stream:               0,
			ToolResultContents:   nil,
			ErrorText:            "",
			ExitCode:             0,
			Failure:              false,
			ToolCall:             presentationdomain.ToolCallState{},
			Models:               nil,
			ModelSelection:       presentationdomain.ModelSelection{},
		})
	}
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  80,
		Height: 7,
	})

	view := model.View().Content
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), 7)
	assert.NotContains(t, view, "oldest")
	assert.Contains(t, view, "newer")
	assert.Contains(t, view, "latest")
	assert.Contains(t, view, "Status: Idle")
	assert.Contains(t, view, "Request: |")
	assert.Contains(t, view, "Ctrl+Q quit")
	assert.Len(t, model.state.Transcript, 5)
}

// TestModelRetainsTranscriptWhenReturningToIdleForSecondTurn verifies editor reuse after settlement.
func TestModelRetainsTranscriptWhenReturningToIdleForSecondTurn(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: "first response",
		}},
		Startup:            nil,
		Extensions:         nil,
		Availability:       0,
		Position:           0,
		ModelContentKind:   0,
		ToolCallID:         "",
		ToolName:           "",
		Status:             "",
		Stream:             0,
		Text:               "",
		ToolResultContents: nil,
		ErrorText:          "",
		ExitCode:           0,
		Failure:            false,
		ToolCall:           presentationdomain.ToolCallState{},
		Models:             nil,
		ModelSelection:     presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventAgentSettled,
		Text:                 "completed",
		Startup:              nil,
		Extensions:           nil,
		Availability:         0,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventAvailability,
		Availability:         presentationdomain.AvailabilityIdle,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "second request",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	assert.Contains(t, model.View().Content, "assistant: first response")
	assert.Contains(t, model.View().Content, "Request: second request|")
}

// TestRenderLineDistinguishesRefusal verifies refusal text has its own terminal prefix.
func TestRenderLineDistinguishesRefusal(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "[refusal] cannot help", renderLine(presentationdomain.Line{
		Kind:     presentationdomain.LineRefusal,
		Text:     "cannot help",
		ToolName: "",

		Status:             "",
		ToolResultContents: nil,
	}))
}

// newSelectionTestModel builds a model with configured selections and deterministic presentation behavior.
func newSelectionTestModel(t *testing.T, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind:         presentationdomain.EventInitialization,
		Availability: availability,
		Models: []presentationdomain.ConfiguredModel{
			{
				ProviderID: "openai-codex",
				ModelID:    "gpt",
				Reasoning:  testReasoning(presentationdomain.ReasoningChoiceLow, presentationdomain.ReasoningChoiceHigh),
			},
			{
				ProviderID: "openrouter",
				ModelID:    "sonnet",
				Reasoning:  testReasoning(presentationdomain.ReasoningChoiceOff),
			},
		},
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID:      "openai-codex",
			ModelID:         "gpt",
			ReasoningChoice: presentationdomain.ReasoningChoiceLow,
		},
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
	}, service.Apply, emit)
	model.state.Transcript = []presentationdomain.Line{{
		Kind:               presentationdomain.LineModel,
		Text:               "existing",
		ToolName:           "",
		Status:             "",
		ToolResultContents: nil,
	}}
	return model
}

func newTestModel(t *testing.T, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	if emit == nil {
		emit = func(presentationdomain.Command) error { return nil }
	}
	return NewModel(presentationdomain.Event{
		Kind:                 presentationdomain.EventInitialization,
		Availability:         availability,
		Startup:              nil,
		Extensions:           nil,
		Position:             0,
		ModelContentKind:     0,
		ModelResponseContent: nil,
		ToolCallID:           "",
		ToolName:             "",
		Status:               "",
		Stream:               0,
		Text:                 "",
		ToolResultContents:   nil,
		ErrorText:            "",
		ExitCode:             0,
		Failure:              false,
		ToolCall:             presentationdomain.ToolCallState{},
		Models:               nil,
		ModelSelection:       presentationdomain.ModelSelection{},
	}, service.Apply, emit)
}

// updateModel applies one Bubble Tea message and requires the concrete model result.
func updateModel(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	next, _ := model.Update(message)
	return next.(Model)
}

// executeCommand applies one key and executes its emitted acknowledgement command.
func executeCommand(t *testing.T, model Model, key tea.KeyPressMsg) Model {
	t.Helper()
	next, command := model.Update(key)
	model = next.(Model)
	require.NotNil(t, command)
	return updateModel(t, model, command())
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
