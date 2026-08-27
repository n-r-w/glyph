package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"
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

	// Arrange one configured model and every model or reasoning cycle key.
	service := presentationusecase.New()
	// Act by applying each cycle key to a single-selection model.
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
			RestoredTranscript: nil,
			Kind:               presentationdomain.EventInitialization,
			Availability:       mo.Some(presentationdomain.AvailabilityIdle),
			Models: []presentationdomain.ConfiguredModel{{
				ProviderID: "openai-codex",
				ModelID:    "gpt",
				Reasoning:  testReasoning(presentationdomain.ReasoningChoiceHigh),
			}},
			ModelSelection: mo.Some(presentationdomain.ModelSelection{
				ProviderID:      "openai-codex",
				ModelID:         "gpt",
				ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
			}),
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
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		}, service.Apply, func(presentationdomain.Command) error {
			t.Fatal("redundant selection command emitted")
			return nil
		})

		_, command := model.Update(tea.KeyPressMsg(key))
		// Assert redundant selection never emits a command.
		assert.Nil(t, command)
	}
}

// TestModelFixedReasoningHidesSelectionAndKeepsDisplayState verifies fixed-choice selection and local display state.
func TestModelFixedReasoningHidesSelectionAndKeepsDisplayState(t *testing.T) {
	t.Parallel()

	// Arrange a model whose only reasoning choice is fixed.
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Availability:       mo.Some(presentationdomain.AvailabilityIdle),
		Models: []presentationdomain.ConfiguredModel{{
			ProviderID: "ollama",
			ModelID:    "ornith",
			Reasoning:  testReasoning(presentationdomain.ReasoningChoiceOn),
		}},
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		}),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}, service.Apply, func(presentationdomain.Command) error {
		t.Fatal("fixed reasoning selection command emitted")
		return nil
	})

	assert.NotContains(t, model.View().Content, "Shift+Tab reasoning")
	// Act by applying reasoning toggle and transcript events.
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
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelSelectionChanged,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	// Assert fixed reasoning stays hidden while display state remains intact.
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
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelSelectionChanged,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
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
		ProviderID:      mo.Some("openrouter"),
		ModelID:         mo.Some("sonnet"),
		Text:            mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
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

	// Arrange eight configured models in a constrained terminal.
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
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Availability:       mo.Some(presentationdomain.AvailabilityIdle),
		Models:             models,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "provider",
			ModelID:         "model-0",
			ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
		}),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}, service.Apply, nil)
	model.height = 10
	model.input = []rune("draft")
	model.cursor = len(model.input)
	model.state.Transcript = []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("first"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("second"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}
	originalTranscript := slices.Clone(model.state.Transcript)

	// Act by opening the selector at the first configured model.
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'l',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	view := model.View().Content
	// Assert the initial selector page fits and hides rows below the viewport.
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
	// Act by navigating from the first selector row to the last.
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

	// Assert navigation reaches the final row without changing the draft or transcript.
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

	// Arrange a running model with command capture.
	var commands []presentationdomain.Command
	model := newSelectionTestModel(t, presentationdomain.AvailabilityRunning, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	// Act by cycling provider, model, and reasoning choices during the run.
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

	// Assert the model emits selection commands but waits for host confirmation before display changes.
	assert.Equal(t, []presentationdomain.Command{
		{
			Kind:            presentationdomain.CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            presentationdomain.CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            presentationdomain.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(presentationdomain.ReasoningChoiceHigh),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
	}, commands)
	assert.Equal(t, mo.Some(presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceLow,
	}), model.state.ModelSelection)
	assert.Contains(t, model.View().Content, "openai-codex / gpt / low")
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventError,
		Text:                 mo.Some("selection failed"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	assert.Contains(t, model.View().Content, "openai-codex / gpt / low")
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelSelectionChanged,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "openai-codex",
			ModelID:         "gpt",
			ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
		}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})
	assert.Contains(t, model.View().Content, "openai-codex / gpt / high")
}

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
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityAuthenticationFailed),
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
		},
		{
			Kind:            presentationdomain.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:       presentationdomain.CommandQuit,
			Text:       mo.None[string](),
			ProviderID: mo.None[string](),

			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
	}, commands)
}

// TestModelRendersWarningAndExtensionIdentityPath verifies startup warning and path visibility.
func TestModelRendersWarningAndExtensionIdentityPath(t *testing.T) {
	t.Parallel()

	// Arrange startup warnings and extension identity paths.
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
			{
				Kind:     presentationdomain.LineWarning,
				Text:     mo.Some("excluded UI optional at /plugins/ui/optional"),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			},
			{
				Kind:     presentationdomain.LineInformation,
				Text:     mo.Some("UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read"),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			},
		},
		Extensions: []presentationdomain.Extension{{
			ID:    "glyph-tools",
			Path:  "/plugins/extension/glyph-tools",
			Tools: []string{"read"},
		}},
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
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
	}, service.Apply, func(presentationdomain.Command) error { return nil })

	// Act by rendering the initialized model.
	view := model.View().Content
	// Assert the view shows each warning and extension path once.
	assert.Contains(t, view, "[warning] excluded UI optional at /plugins/ui/optional")
	assert.Contains(t, view, "[info] UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read")
	assert.Equal(t, 1, strings.Count(view, "glyph-tools at /plugins/extension/glyph-tools"))
}

// TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize verifies complete view composition.
func TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize(t *testing.T) {
	t.Parallel()

	// Arrange startup content, transcript, active output, authorization, and window events.
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{{
			Kind:     presentationdomain.LineInformation,
			Text:     mo.Some("Glyph session initialized."),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		}},
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
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
	}, service.Apply, func(presentationdomain.Command) error { return nil })
	// Act by applying lifecycle updates and resizing the terminal.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInformation,
		Text:                 mo.Some("Ready."),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("Working"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAuthorization,
		Text:                 mo.Some("https://example.test/oauth"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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

	// Assert the alternate-screen view contains each projected state and fits the new size.
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

	// Arrange streamed model text and a terminal response at another position.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	// Act by applying the terminal response after streamed text.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(0),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("complete answer"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		Position:           mo.None[int](),
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("complete answer"),
		}},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})

	// Assert the model clears active fragments and renders the completed text once.
	assert.Empty(t, model.state.ActiveModel)
	assert.Equal(t, 1, strings.Count(model.View().Content, "complete answer"))
}

// TestModelRendersProvisionalToolCallNameFieldsAndPrefix verifies provisional complete and prefix fields remain visible.
func TestModelRendersProvisionalToolCallNameFieldsAndPrefix(t *testing.T) {
	t.Parallel()

	// Arrange a provisional tool call with complete and prefix fields.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, func(presentationdomain.Command) error { return nil })
	// Act by applying and rendering the provisional call.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventToolCallPreview,
		ToolCall: mo.Some(presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: true,
			Fields: []presentationdomain.ToolCallField{
				{
					Name:   "path",
					Value:  mo.Some[any]("file.txt"),
					Prefix: mo.None[string](),
				},
				{
					Name:   "query",
					Prefix: mo.Some("hel"),
					Value:  mo.None[any](),
				},
			},
			Arguments: nil,
		}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})

	view := model.View().Content
	// Assert the view includes the call name, complete field, and prefix field.
	assert.Contains(t, view, "[tool:call] read (provisional)")
	assert.Contains(t, view, `path="file.txt"`)
	assert.Contains(t, view, "query=hel")
	assert.NotContains(t, view, `{"path"`)
}

// TestModelReasoningUsesOneLocalCollapsedToggle verifies ordered markers, one shared toggle, and wrapped expansion.
func TestModelReasoningUsesOneLocalCollapsedToggle(t *testing.T) {
	t.Parallel()

	// Arrange reasoning transcript lines in a narrow terminal.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model.state.Transcript = append(model.state.Transcript,
		presentationdomain.Line{
			Kind:     presentationdomain.LineReasoning,
			Text:     mo.Some("first reasoning block"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		presentationdomain.Line{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("between blocks"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		presentationdomain.Line{
			Kind:     presentationdomain.LineReasoning,
			Text:     mo.Some("second reasoning block"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
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
	// Act by toggling reasoning expansion and rendering the expanded lines.
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
	// Assert expanded reasoning remains within terminal width.
	for _, line := range expandedLines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 12)
	}
}

// TestModelWrapsCompletedUnicodeContent verifies readable wrapping, display width, and embedded line boundaries.
func TestModelWrapsCompletedUnicodeContent(t *testing.T) {
	t.Parallel()

	// Arrange completed mixed Unicode content in a narrow terminal.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("readable words wrap cleanly\n你好 世界"),
		}},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		Position:          mo.None[int](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: 0,
	})

	// Act by computing visible wrapped body lines.
	lines := model.visibleBodyLines(0)
	// Assert completed content wraps at cell boundaries without corrupting Unicode.
	assert.Equal(t, []string{"assistant:", "readable words", "wrap cleanly", "你好 世界"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelWrapsActiveContent verifies word wrapping and long-token splitting for active streaming text.
func TestModelWrapsActiveContent(t *testing.T) {
	t.Parallel()

	// Arrange active model content with a long unbroken word.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("active words and supercalifragilistic"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: 0,
	})

	// Act by computing visible wrapped body lines.
	lines := model.visibleBodyLines(0)
	// Assert active content wraps words and clips long tokens to terminal width.
	assert.Equal(t, []string{"assistant:", "active words and", "supercalifragili", "stic"}, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 16)
	}
}

// TestModelClipsAfterWrapping verifies that the height budget selects wrapped visual lines.
func TestModelClipsAfterWrapping(t *testing.T) {
	t.Parallel()

	// Arrange wrapped active content and two terminal heights.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("active words and supercalifragilistic"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	// Act by resizing after content wrapping.
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  16,
		Height: fixedViewLineCount + 2,
	})
	// Assert clipping retains only lines that fit each terminal height.
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

	// Arrange five transcript lines, an editor draft, and a short terminal.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	for _, text := range []string{"oldest", "older", "middle", "newer", "latest"} {
		model = updateModel(t, model, presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventInformation,
			Text:                 mo.Some(text),
			Startup:              nil,
			Extensions:           nil,
			Availability:         mo.None[presentationdomain.Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
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
	}
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  80,
		Height: 7,
	})

	// Act by rendering the constrained terminal view.
	view := model.View().Content
	// Assert the editor and latest transcript remain visible while older lines are clipped.
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

	// Arrange a completed first response followed by an idle second draft.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	// Act by settling the first turn and entering the second request.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("first response"),
		}},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		Position:          mo.None[int](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAgentSettled,
		Text:                 mo.Some("completed"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventAvailability,
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
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
		Text:        "second request",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	// Assert the first transcript and second draft remain visible together.
	assert.Contains(t, model.View().Content, "assistant: first response")
	assert.Contains(t, model.View().Content, "Request: second request|")
}

// TestRenderLineDistinguishesRefusal verifies refusal text has its own terminal prefix.
func TestRenderLineDistinguishesRefusal(t *testing.T) {
	t.Parallel()

	// Arrange a refusal transcript line.
	line := presentationdomain.Line{
		Kind:     presentationdomain.LineRefusal,
		Text:     mo.Some("cannot help"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}

	// Act by rendering the refusal line.
	result := renderLine(line)

	// Assert the rendered prefix distinguishes refusal from model text.
	assert.Equal(t, "[refusal] cannot help", result)
}

// TestModelClearsSessionCommandOnlyAfterHostConfirmsReplacement verifies rejected replacement keeps the draft until confirmation.
func TestModelClearsSessionCommandOnlyAfterHostConfirmsReplacement(t *testing.T) {
	t.Parallel()

	// Arrange a pending session command and delayed host confirmation.
	commands := make([]presentationdomain.Command, 0, 1)
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model.input = []rune("/new")
	model.cursor = len(model.input)

	// Act by submitting the command and then applying confirmed session replacement.
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 1)
	assert.Equal(t, presentationdomain.CommandCreateSession, commands[0].Kind)
	assert.Equal(t, "/new", string(model.input))

	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventSessionChanged,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		SessionInfo: mo.Some(presentationdomain.SessionInfo{
			ID:               "new-session",
			Name:             "",
			NamePresent:      false,
			WorkingDirectory: "/project",
			StoragePath:      "",
			StoragePresent:   false,
			CreatedAt:        time.Unix(1, 0),
			UpdatedAt:        time.Unix(1, 0),
		}),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	// Assert the draft clears only after host confirmation.
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)

	model.input = []rune("/session")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	assert.Equal(t, "/session", string(model.input))
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventSessionInformation,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		SessionInfo: mo.Some(presentationdomain.SessionInfo{
			ID:               "new-session",
			Name:             "renamed",
			NamePresent:      true,
			WorkingDirectory: "/project",
			StoragePath:      "/sessions/new-session.jsonl",
			StoragePresent:   true,
			CreatedAt:        time.Unix(1, 0),
			UpdatedAt:        time.Unix(2, 0),
		}),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
	view := model.View().Content
	assert.Contains(t, view, "Session ID: new-session")
	assert.Contains(t, view, "Name: renamed")
	assert.Contains(t, view, "Working directory: /project")
	assert.Contains(t, view, "Storage path: /sessions/new-session.jsonl")
	assert.Contains(t, view, "Created: 1970-01-01T00:00:01Z")
	assert.Contains(t, view, "Updated: 1970-01-01T00:00:02Z")
}

// TestModelResumeSelectorEmitsSelectedSession verifies selector navigation emits the chosen ID without mutating the draft.
func TestModelResumeSelectorEmitsSelectedSession(t *testing.T) {
	t.Parallel()

	// Arrange a resume selector with two sessions and a preserved draft.
	commands := make([]presentationdomain.Command, 0, 2)
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model.input = []rune("/resume")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 1)
	assert.Equal(t, presentationdomain.CommandListSessions, commands[0].Kind)
	model.resumeStatus = "stale rejection"

	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventSessionList,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		Sessions: []presentationdomain.SessionSummary{
			{Info: presentationdomain.SessionInfo{ID: "first", Name: "first", NamePresent: true, WorkingDirectory: "/project", StoragePath: "/first", StoragePresent: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0)}, FirstUserText: "", TextPresent: false, TotalMessages: 1},
			{Info: presentationdomain.SessionInfo{ID: "second", Name: "", NamePresent: false, WorkingDirectory: "/project", StoragePath: "/second", StoragePresent: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(3, 0)}, FirstUserText: "fallback", TextPresent: true, TotalMessages: 2},
			{Info: presentationdomain.SessionInfo{ID: "id-fallback", Name: "", NamePresent: false, WorkingDirectory: "/project", StoragePath: "/third", StoragePresent: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(4, 0)}, FirstUserText: "", TextPresent: false, TotalMessages: 0},
		},
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	assert.True(t, model.selectorOpen)
	assert.True(t, model.sessionSelector)
	assert.Empty(t, model.resumeStatus)
	model.width = 100
	assert.Contains(t, model.View().Content, "Sessions:")
	assert.Contains(t, model.View().Content, "id-fallback")
	assert.Equal(t, "/resume", string(model.input))

	model.state.SessionInfo = mo.Some(presentationdomain.SessionInfo{
		ID: "active", Name: "active", NamePresent: true, WorkingDirectory: "/project",
		StoragePath: "/active", StoragePresent: true, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(5, 0),
	})
	model.state.Transcript = []presentationdomain.Line{{
		Kind: presentationdomain.LineInformation, ToolName: mo.None[string](), Status: mo.None[string](),
		Text: mo.Some("existing transcript"), Contents: mo.None[[]presentationdomain.Content](),
	}}
	model.input = []rune("preserved draft")
	model.cursor = len(model.input)
	// Act by selecting the second session and confirming resume.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyDown)))
	beforeSessions := append([]presentationdomain.SessionSummary(nil), model.state.Sessions...)
	beforeTranscript := append([]presentationdomain.Line(nil), model.state.Transcript...)
	beforeInfo := model.state.SessionInfo
	beforeInput := append([]rune(nil), model.input...)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	// Assert the selected session command is emitted without mutating the selector data or draft.
	require.Len(t, commands, 2)
	assert.Equal(t, presentationdomain.CommandResumeSession, commands[1].Kind)
	assert.Equal(t, "second", commands[1].SessionID.MustGet())
	assert.True(t, model.selectorOpen)
	assert.True(t, model.sessionSelector)
	assert.True(t, model.resumePending)
	assert.Equal(t, 1, model.selectorRow)
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	assert.Len(t, commands, 2)

	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInformation, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.Some("session persistence failed"),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	assert.True(t, model.selectorOpen)
	assert.True(t, model.sessionSelector)
	assert.False(t, model.resumePending)
	assert.Equal(t, 1, model.selectorRow)
	assert.Equal(t, beforeSessions, model.state.Sessions)
	assert.Equal(t, beforeTranscript, model.state.Transcript)
	assert.Equal(t, beforeInfo, model.state.SessionInfo)
	assert.Equal(t, beforeInput, model.input)
	assert.Contains(t, model.View().Content, "session persistence failed")

	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 3)
	assert.Equal(t, presentationdomain.CommandResumeSession, commands[2].Kind)
	assert.True(t, model.resumePending)
	assert.NotContains(t, model.View().Content, "session persistence failed")

	restored := []presentationdomain.Line{
		{
			Kind: presentationdomain.LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some("prior-user"), Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind: presentationdomain.LineModel, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some("prior-model"), Contents: mo.None[[]presentationdomain.Content](),
		},
	}
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: restored,
		Kind:               presentationdomain.EventSessionChanged, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.Some(model.state.Sessions[1].Info), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})
	assert.False(t, model.selectorOpen)
	assert.False(t, model.sessionSelector)
	assert.Equal(t, restored, model.state.Transcript)
	assert.Empty(t, model.resumeStatus)
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
}

// TestModelResumeRejectionReservesHeightAndFitsTerminalWidth verifies status layout consumes height without overflow.
func TestModelResumeRejectionReservesHeightAndFitsTerminalWidth(t *testing.T) {
	t.Parallel()

	// Arrange an open resume selector with rejection text, constrained dimensions, and stored-session rows.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error { return nil })
	model.selectorOpen = true
	model.sessionSelector = true
	model.resumeStatus = "Session replacement is unavailable because another operation is active."
	model.width = 24
	model.height = fixedViewLineCount + selectorFixedLineCount + 1 + 2
	for index := range 5 {
		model.state.Sessions = append(model.state.Sessions, presentationdomain.SessionSummary{
			Info: presentationdomain.SessionInfo{
				ID: fmt.Sprintf("stored-%d", index), Name: "", NamePresent: false, WorkingDirectory: "/project",
				StoragePath: "/stored", StoragePresent: true,
				CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(int64(index+2), 0),
			},
			FirstUserText: "", TextPresent: false, TotalMessages: 0,
		})
	}

	// Act by calculating the visible selector lines.
	lines := model.visibleSelectorLines()

	// Assert the status reserves one line, remains visible, and fits the terminal cell width.
	require.Len(t, lines, selectorFixedLineCount+1+2)
	assert.Contains(t, lines[len(lines)-2], "Session status:")
	assert.LessOrEqual(t, ansi.StringWidth(lines[len(lines)-2]), model.width)
}

// TestModelEscapeClearsResumeRejection verifies Escape closes the selector and clears rejection and draft state.
func TestModelEscapeClearsResumeRejection(t *testing.T) {
	t.Parallel()

	// Arrange an open resume selector with rejection text, a resume draft, and one stored session.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error { return nil })
	model.selectorOpen = true
	model.sessionSelector = true
	model.resumeStatus = "Session replacement is unavailable."
	model.input = []rune("/resume")
	model.cursor = len(model.input)
	model.state.Sessions = []presentationdomain.SessionSummary{{
		Info: presentationdomain.SessionInfo{
			ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
			StoragePath: "/stored", StoragePresent: true,
			CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
		},
		FirstUserText: "", TextPresent: false, TotalMessages: 0,
	}}

	// Act by sending Escape through the model update path.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEscape)))

	// Assert selector state, rejection text, and draft input are cleared from state and view.
	assert.False(t, model.selectorOpen)
	assert.False(t, model.sessionSelector)
	assert.Empty(t, model.resumeStatus)
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
	assert.NotContains(t, model.View().Content, "Session replacement is unavailable.")
}

// TestFormatSessionInfoShowsAbsentOptionalFields verifies absent name and storage path use explicit placeholders.
func TestFormatSessionInfoShowsAbsentOptionalFields(t *testing.T) {
	t.Parallel()

	// Arrange session information whose optional name and storage path are absent.

	// Act by formatting that session information for display.
	text := formatSessionInfo(presentationdomain.SessionInfo{
		ID: "startup", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	})

	// Assert both absent optional fields use the explicit placeholder.
	assert.Contains(t, text, "Name: <absent>")
	assert.Contains(t, text, "Storage path: <absent>")
}

// TestEllipsizeUsesTerminalCellWidth verifies wide runes respect the cell-width limit.
func TestEllipsizeUsesTerminalCellWidth(t *testing.T) {
	t.Parallel()

	// Arrange wide Unicode text and a four-cell limit.
	text := "界界界"

	// Act by ellipsizing the text to the terminal width.
	result := ellipsize(text, 4)

	// Assert the result fits the cell limit and ends with an ellipsis.
	assert.LessOrEqual(t, ansi.StringWidth(result), 4)
	assert.True(t, strings.HasSuffix(result, "…"))
}

// newSelectionTestModel builds a model with configured selections and deterministic presentation behavior.
func newSelectionTestModel(t *testing.T, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Availability:       mo.Some(availability),
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
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "openai-codex",
			ModelID:         "gpt",
			ReasoningChoice: presentationdomain.ReasoningChoiceLow,
		}),
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
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}, service.Apply, emit)
	model.state.Transcript = []presentationdomain.Line{{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("existing"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
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
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInitialization,
		Availability:         mo.Some(availability),
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
	}, service.Apply, emit)
}

// testKey builds one unmodified Bubble Tea key for controller tests.
func testKey(code rune) tea.Key {
	return tea.Key{
		Code: code, Text: "", Mod: 0, ShiftedCode: 0, BaseCode: 0, IsRepeat: false,
	}
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
