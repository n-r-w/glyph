//go:build !integration

package presentation

import (
	"fmt"
	"slices"
	"testing"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
)

// TestModelSelectorConfirmsAndCancelsWithoutChangingDraftOrTranscript verifies modal behavior.
func TestModelSelectorConfirmsAndCancelsWithoutChangingDraftOrTranscript(t *testing.T) {
	t.Parallel()

	var commands []Command
	model := newSelectionTestModel(
		t,
		AvailabilityIdle,
		func(command Command) error {
			commands = append(commands, command)
			return nil
		},
	)
	model.model.input = []rune("draft")
	model.model.cursor = len(model.model.input)
	originalTranscript := slices.Clone(model.model.state.Transcript)

	model = updateModel(t, model, inputcontroller.Key{
		Code: 'l',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	assert.True(t, model.model.selectorOpen)
	assert.Equal(t, "gpt", model.model.state.ModelSelection.OrEmpty().ModelID)
	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyDown,
		Text: "",
		Mod:  0,
	})
	model = executeCommand(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyEnter,
		Text: "",
		Mod:  0,
	})
	assert.False(t, model.model.selectorOpen)
	assert.Equal(t, []Command{{
		Kind:            CommandSelectModel,
		ProviderID:      mo.Some("openrouter"),
		ModelID:         mo.Some("sonnet"),
		Text:            mo.None[string](),
		ReasoningChoice: mo.None[ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[TreeCommand](),
	}}, commands)
	assert.Equal(t, "draft", string(model.model.input))
	assert.Equal(t, originalTranscript, model.model.state.Transcript)

	model = updateModel(t, model, inputcontroller.Key{
		Code: 'l',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyEscape,
		Text: "",
		Mod:  0,
	})
	assert.False(t, model.model.selectorOpen)
	assert.Len(t, commands, 1)
	assert.Equal(t, "draft", string(model.model.input))
	assert.Equal(t, originalTranscript, model.model.state.Transcript)
}

// TestModelSelectorKeepsEveryRowReachable verifies constrained selector rendering.
func TestModelSelectorKeepsEveryRowReachable(t *testing.T) {
	t.Parallel()

	// Arrange eight configured models in a constrained terminal.
	models := make([]ConfiguredModel, 8)
	for index := range models {
		models[index] = ConfiguredModel{
			ProviderID: "provider",
			ModelID:    fmt.Sprintf("model-%d", index),
			Reasoning:  testReasoning(ReasoningChoiceHigh),
		}
	}
	model := newTestApplication(event{
		FailureCode:        "",
		RestoredTranscript: nil,
		Kind:               eventInitialization,
		Availability:       mo.Some(AvailabilityIdle),
		Models:             models,
		ModelSelection: mo.Some(ModelSelection{
			ProviderID:      "provider",
			ModelID:         "model-0",
			ReasoningChoice: ReasoningChoiceHigh,
		}),
		Startup:              nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	}, testMockHost(t, nil))
	model.model.input = []rune("draft")
	model.model.cursor = len(model.model.input)
	model.model.state.Transcript = []Line{
		{
			Kind:     LineModel,
			Text:     mo.Some("first"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineModel,
			Text:     mo.Some("second"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
	}
	originalTranscript := slices.Clone(model.model.state.Transcript)

	// Act by opening the selector at the first configured model.
	model = updateModel(t, model, inputcontroller.Key{
		Code: 'l',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	// Assert the selector starts at its Host-confirmed model.
	assert.Zero(t, model.model.selectorRow)

	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyUp,
		Text: "",
		Mod:  0,
	})
	assert.Equal(t, 7, model.model.selectorRow)
	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyDown,
		Text: "",
		Mod:  0,
	})
	assert.Zero(t, model.model.selectorRow)
	// Act by navigating from the first selector row to the last.
	for range 7 {
		model = updateModel(t, model, inputcontroller.Key{
			Code: inputcontroller.KeyDown,
			Text: "",
			Mod:  0,
		})
	}

	// Assert navigation reaches the final row without changing the draft or transcript.
	assert.Equal(t, 7, model.model.selectorRow)
	assert.Equal(t, "draft", string(model.model.input))
	assert.Equal(t, originalTranscript, model.model.state.Transcript)
}

// TestTypedModelCommandIsConsumedWhenOpeningSelector verifies the command does not enter transcript.
func TestTypedModelCommandIsConsumedWhenOpeningSelector(t *testing.T) {
	t.Parallel()

	model := newSelectionTestModel(t, AvailabilityIdle, nil)
	originalTranscript := slices.Clone(model.model.state.Transcript)
	model = updateModel(t, model, inputcontroller.Key{
		Text: "/model",
		Mod:  0,
		Code: 0,
	})
	model = updateModel(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyEnter,
		Text: "",
		Mod:  0,
	})

	assert.True(t, model.model.selectorOpen)
	assert.Empty(t, model.model.input)
	assert.Equal(t, originalTranscript, model.model.state.Transcript)
}

// TestModelSelectionCyclingWorksDuringRun verifies modifier data and configured wrap order.
func TestModelSelectionCyclingWorksDuringRun(t *testing.T) {
	t.Parallel()

	// Arrange a running model with command capture.
	var commands []Command
	model := newSelectionTestModel(
		t,
		AvailabilityRunning,
		func(command Command) error {
			commands = append(commands, command)
			return nil
		},
	)
	// Act by cycling provider, model, and reasoning choices during the run.
	model = executeCommand(t, model, inputcontroller.Key{
		Code: 'p',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	model = executeCommand(t, model, inputcontroller.Key{
		Code: 'p',
		Mod:  inputcontroller.ModShift | inputcontroller.ModCtrl,
		Text: "",
	})
	model = executeCommand(t, model, inputcontroller.Key{
		Code: inputcontroller.KeyTab,
		Mod:  inputcontroller.ModShift,
		Text: "",
	})

	// Assert the model emits selection commands but waits for host confirmation before display changes.
	assert.Equal(t, []Command{
		{
			Kind:            CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[TreeCommand](),
		},
		{
			Kind:            CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[TreeCommand](),
		},
		{
			Kind:            CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(ReasoningChoiceHigh),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[TreeCommand](),
		},
	}, commands)
	assert.Equal(t, mo.Some(ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: ReasoningChoiceLow,
	}), model.model.state.ModelSelection)
	assert.Equal(t, ReasoningChoiceLow, model.model.state.ModelSelection.OrEmpty().ReasoningChoice)
	model = updateModel(t, model, testPresentationEvent(eventError, mo.Some("selection failed"), mo.None[int]()))
	assert.Equal(t, ReasoningChoiceLow, model.model.state.ModelSelection.OrEmpty().ReasoningChoice)
	confirmed := newEvent(eventModelSelectionChanged)
	confirmed.ModelSelection = mo.Some(ModelSelection{
		ProviderID: "openai-codex", ModelID: "gpt", ReasoningChoice: ReasoningChoiceHigh,
	})
	model = updateModel(t, model, confirmed)
	assert.Equal(t, ReasoningChoiceHigh, model.model.state.ModelSelection.OrEmpty().ReasoningChoice)
}
