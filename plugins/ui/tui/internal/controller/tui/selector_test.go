package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

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
