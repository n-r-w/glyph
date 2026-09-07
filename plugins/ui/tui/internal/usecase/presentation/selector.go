package presentation

import (
	"slices"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
)

// openSelector highlights the current model without changing editor or transcript state.
func (model interaction) openSelector() (interaction, *commandIntent) {
	if len(model.state.Models) == 0 {
		return model, nil
	}
	model.selectorOpen = true
	model.selectorRow = model.currentModelIndex()
	return model, nil
}

// updateSelector handles only modal navigation, confirmation, and cancellation.
func (model interaction) updateSelector(key inputcontroller.Key) (interaction, *commandIntent) {
	rowCount := len(model.state.Models)
	if model.sessionSelector {
		rowCount = len(model.state.Sessions)
	}
	if rowCount == 0 {
		if key.Code == inputcontroller.KeyEscape {
			model = model.cancelSelector()
		}
		return model, nil
	}
	if model.sessionSelector && model.resumePending {
		if key.Code == inputcontroller.KeyEscape {
			model = model.cancelSelector()
		}
		return model, nil
	}
	switch key.Code {
	case inputcontroller.KeyUp:
		model.selectorRow = (model.selectorRow - 1 + rowCount) % rowCount
	case inputcontroller.KeyDown:
		model.selectorRow = (model.selectorRow + 1) % rowCount
	case inputcontroller.KeyEnter:
		if model.sessionSelector {
			selected := model.state.Sessions[model.selectorRow]
			// SessionChanged or Escape owns selector closure so a rejected resume preserves user state.
			model.resumePending = true
			model.resumeStatus = ""
			return model.emitSessionCommand(CommandResumeSession, selected.Info.ID, "")
		}
		selected := model.state.Models[model.selectorRow]
		model.selectorOpen = false
		return model.emitCommand(modelSelectionCommand(selected))
	case inputcontroller.KeyEscape:
		model = model.cancelSelector()
	}
	return model, nil
}

// cancelSelector discards a resume draft only when the user cancels its selector.
func (model interaction) cancelSelector() interaction {
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
func (model interaction) cycleModel(direction int) (interaction, *commandIntent) {
	if len(model.state.Models) <= 1 {
		return model, nil
	}
	index := (model.currentModelIndex() + direction + len(model.state.Models)) % len(model.state.Models)
	selected := model.state.Models[index]
	return model.emitCommand(modelSelectionCommand(selected))
}

// modelSelectionCommand maps one selectable model to its command payload.
func modelSelectionCommand(selected ConfiguredModel) Command {
	return Command{
		Kind:            CommandSelectModel,
		Text:            mo.None[string](),
		ProviderID:      mo.Some(selected.ProviderID),
		ModelID:         mo.Some(selected.ModelID),
		ReasoningChoice: mo.None[ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[TreeCommand](),
	}
}

// cycleReasoning emits the next configured level for the Host-confirmed model.
func (model interaction) cycleReasoning() (interaction, *commandIntent) {
	if len(model.state.Models) == 0 {
		return model, nil
	}
	configured := model.state.Models[model.currentModelIndex()]
	if len(configured.Reasoning.Choices) <= 1 {
		return model, nil
	}
	selection, ok := model.state.ModelSelection.Get()
	if !ok {
		return model, nil
	}
	index := 0
	if current := slices.Index(configured.Reasoning.Choices, selection.ReasoningChoice); current >= 0 {
		index = (current + 1) % len(configured.Reasoning.Choices)
	}
	return model.emitCommand(Command{
		Kind:            CommandSelectReasoningChoice,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.Some(configured.Reasoning.Choices[index]),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[TreeCommand](),
	})
}

// currentModelIndex resolves the Host-confirmed selection in configured order.
func (model interaction) currentModelIndex() int {
	selection, ok := model.state.ModelSelection.Get()
	if !ok {
		return 0
	}
	index := slices.IndexFunc(model.state.Models, func(configured ConfiguredModel) bool {
		return configured.ProviderID == selection.ProviderID && configured.ModelID == selection.ModelID
	})
	return max(index, 0)
}
