package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// openSelector highlights the current model without changing editor or transcript state.
func (model Model) openSelector() (tea.Model, tea.Cmd) {
	if len(model.state.Models) == 0 {
		return model, nil
	}
	model.selectorOpen = true
	model.selectorRow = model.currentModelIndex()
	return model, nil
}

// updateSelector handles only modal navigation, confirmation, and cancellation.
func (model Model) updateSelector(key tea.Key) (tea.Model, tea.Cmd) {
	rowCount := len(model.state.Models)
	if model.sessionSelector {
		rowCount = len(model.state.Sessions)
	}
	if rowCount == 0 {
		if key.Code == tea.KeyEscape {
			model = model.cancelSelector()
		}
		return model, nil
	}
	if model.sessionSelector && model.resumePending {
		if key.Code == tea.KeyEscape {
			model = model.cancelSelector()
		}
		return model, nil
	}
	switch key.Code {
	case tea.KeyUp:
		model.selectorRow = (model.selectorRow - 1 + rowCount) % rowCount
	case tea.KeyDown:
		model.selectorRow = (model.selectorRow + 1) % rowCount
	case tea.KeyEnter:
		if model.sessionSelector {
			selected := model.state.Sessions[model.selectorRow]
			// SessionChanged or Escape owns selector closure so a rejected resume preserves user state.
			model.resumePending = true
			model.resumeStatus = ""
			return model.emitSessionCommand(presentationdomain.CommandResumeSession, selected.Info.ID, "")
		}
		selected := model.state.Models[model.selectorRow]
		model.selectorOpen = false
		return model.emitCommand(presentationdomain.Command{
			Kind:            presentationdomain.CommandSelectModel,
			Text:            mo.None[string](),
			ProviderID:      mo.Some(selected.ProviderID),
			ModelID:         mo.Some(selected.ModelID),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		})
	case tea.KeyEscape:
		model = model.cancelSelector()
	}
	return model, nil
}

// cancelSelector discards a resume draft only when the user cancels its selector.
func (model Model) cancelSelector() Model {
	if model.sessionSelector {
		model.input = nil
		model.cursor = 0
	}
	model.selectorOpen = false
	model.sessionSelector = false
	model.resumePending = false
	model.resumeStatus = ""
	return model
}

// cycleModel emits the configured neighbor of the Host-confirmed model.
func (model Model) cycleModel(direction int) (tea.Model, tea.Cmd) {
	if len(model.state.Models) <= 1 {
		return model, nil
	}
	index := (model.currentModelIndex() + direction + len(model.state.Models)) % len(model.state.Models)
	selected := model.state.Models[index]
	return model.emitCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSelectModel,
		Text:            mo.None[string](),
		ProviderID:      mo.Some(selected.ProviderID),
		ModelID:         mo.Some(selected.ModelID),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
}

// cycleReasoning emits the next configured level for the Host-confirmed model.
func (model Model) cycleReasoning() (tea.Model, tea.Cmd) {
	if len(model.state.Models) == 0 {
		return model, nil
	}
	configured := model.state.Models[model.currentModelIndex()]
	if len(configured.Reasoning.Choices) <= 1 {
		return model, nil
	}
	index := 0
	selection, ok := model.state.ModelSelection.Get()
	if !ok {
		return model, nil
	}
	for current, level := range configured.Reasoning.Choices {
		if level == selection.ReasoningChoice {
			index = (current + 1) % len(configured.Reasoning.Choices)
			break
		}
	}
	return model.emitCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSelectReasoningChoice,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.Some(configured.Reasoning.Choices[index]),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
}

// currentModelIndex resolves the Host-confirmed selection in configured order.
func (model Model) currentModelIndex() int {
	selection, ok := model.state.ModelSelection.Get()
	if !ok {
		return 0
	}
	index := slices.IndexFunc(model.state.Models, func(configured presentationdomain.ConfiguredModel) bool {
		return configured.ProviderID == selection.ProviderID && configured.ModelID == selection.ModelID
	})
	return max(index, 0)
}
