//go:build !integration

package presentation

import (
	"testing"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelSelectionShortcutsRespectAuthenticationAvailability verifies authentication gates only selection.
func TestModelSelectionShortcutsRespectAuthenticationAvailability(t *testing.T) {
	t.Parallel()

	keys := []inputcontroller.Key{
		{
			Code: 'l',
			Mod:  inputcontroller.ModCtrl,
			Text: "",
		},
		{
			Code: 'p',
			Mod:  inputcontroller.ModCtrl,
			Text: "",
		},
		{
			Code: 'p',
			Mod:  inputcontroller.ModShift | inputcontroller.ModCtrl,
			Text: "",
		},
		{
			Code: inputcontroller.KeyTab,
			Mod:  inputcontroller.ModShift,
			Text: "",
		},
	}
	for _, availability := range []Availability{
		AvailabilityChecking,
		AvailabilityAuthenticating,
	} {
		for _, key := range keys {
			model := newSelectionTestModel(t, availability, nil)
			next, command := updateApplication(model, key)
			updated := next
			assert.Nil(t, command)
			assert.False(t, updated.model.selectorOpen)
		}
	}
	for _, availability := range []Availability{
		AvailabilityIdle,
		AvailabilityRunning,
		AvailabilityAuthenticationFailed,
	} {
		for _, key := range keys {
			model := newSelectionTestModel(t, availability, nil)
			next, command := updateApplication(model, key)
			updated := next
			if key.Code == 'l' {
				assert.True(t, updated.model.selectorOpen)
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
	// Act by applying each cycle key to a single-selection model.
	for _, key := range []inputcontroller.Key{
		{
			Code: 'p',
			Mod:  inputcontroller.ModCtrl,
			Text: "",
		},
		{
			Code: 'p',
			Mod:  inputcontroller.ModShift | inputcontroller.ModCtrl,
			Text: "",
		},
		{
			Code: inputcontroller.KeyTab,
			Mod:  inputcontroller.ModShift,
			Text: "",
		},
	} {
		model := newTestApplication(testEvent(testEventPayload{
			Kind:                 eventInitialization,
			Availability:         mo.Some(AvailabilityIdle),
			Position:             mo.None[int](),
			Text:                 mo.None[string](),
			ModelResponseContent: nil,
			ModelSelection: mo.Some(ModelSelection{
				ProviderID:      "openai-codex",
				ModelID:         "gpt",
				ReasoningChoice: ReasoningChoiceHigh,
			}),
			SessionInfo: mo.None[SessionInfo](),
		}, ConfiguredModel{
			ProviderID: "openai-codex",
			ModelID:    "gpt",
			Reasoning:  testReasoning(ReasoningChoiceHigh),
		}), testMockHost(t, func(Command) error {
			t.Fatal("redundant selection command emitted")
			return nil
		}))

		_, command := updateApplication(model, key)
		// Assert redundant selection never emits a command.
		assert.Nil(t, command)
	}
}

// TestModelFixedReasoningHidesSelectionAndKeepsDisplayState verifies fixed-choice selection and local display state.
func TestModelFixedReasoningHidesSelectionAndKeepsDisplayState(t *testing.T) {
	t.Parallel()

	// Arrange a model whose only reasoning choice is fixed.
	model := newTestApplication(testEvent(testEventPayload{
		Kind:                 eventInitialization,
		Availability:         mo.Some(AvailabilityIdle),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection: mo.Some(ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: ReasoningChoiceOn,
		}),
		SessionInfo: mo.None[SessionInfo](),
	}, ConfiguredModel{
		ProviderID: "ollama",
		ModelID:    "ornith",
		Reasoning:  testReasoning(ReasoningChoiceOn),
	}), testMockHost(t, func(Command) error {
		t.Fatal("fixed reasoning selection command emitted")
		return nil
	}))

	assert.False(t, displaySnapshot(t, model).ReasoningSelectionVisible)
	// Act by applying reasoning toggle and transcript events.
	next, command := updateApplication(model, inputcontroller.Key{
		Code: inputcontroller.KeyTab,
		Mod:  inputcontroller.ModShift,
		Text: "",
	})
	model = next
	assert.Nil(t, command)
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 eventModelSelectionChanged,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection: mo.Some(ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: ReasoningChoiceOn,
		}),
		SessionInfo: mo.None[SessionInfo](),
	}))
	// Assert fixed reasoning stays hidden while display state remains intact.
	assert.False(t, model.model.reasoningExpanded)
	model = updateModel(t, model, inputcontroller.Key{
		Code: 't',
		Mod:  inputcontroller.ModCtrl,
		Text: "",
	})
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 eventModelSelectionChanged,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection: mo.Some(ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: ReasoningChoiceOn,
		}),
		SessionInfo: mo.None[SessionInfo](),
	}))
	assert.True(t, model.model.reasoningExpanded)
}
