package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

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
		model := NewModel(testEvent(testEventPayload{
			Kind:                 presentationdomain.EventInitialization,
			Availability:         mo.Some(presentationdomain.AvailabilityIdle),
			Position:             mo.None[int](),
			Text:                 mo.None[string](),
			ModelResponseContent: nil,
			ModelSelection: mo.Some(presentationdomain.ModelSelection{
				ProviderID:      "openai-codex",
				ModelID:         "gpt",
				ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
			}),
			SessionInfo: mo.None[presentationdomain.SessionInfo](),
		}, presentationdomain.ConfiguredModel{
			ProviderID: "openai-codex",
			ModelID:    "gpt",
			Reasoning:  testReasoning(presentationdomain.ReasoningChoiceHigh),
		}), service.Apply, func(presentationdomain.Command) error {
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
	model := NewModel(testEvent(testEventPayload{
		Kind:                 presentationdomain.EventInitialization,
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		}),
		SessionInfo: mo.None[presentationdomain.SessionInfo](),
	}, presentationdomain.ConfiguredModel{
		ProviderID: "ollama",
		ModelID:    "ornith",
		Reasoning:  testReasoning(presentationdomain.ReasoningChoiceOn),
	}), service.Apply, func(presentationdomain.Command) error {
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
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventModelSelectionChanged,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		}),
		SessionInfo: mo.None[presentationdomain.SessionInfo](),
	}))
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
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventModelSelectionChanged,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.None[string](),
		ModelResponseContent: nil,
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "ollama",
			ModelID:         "ornith",
			ReasoningChoice: presentationdomain.ReasoningChoiceOn,
		}),
		SessionInfo: mo.None[presentationdomain.SessionInfo](),
	}))
	assert.True(t, model.reasoningExpanded)
}
