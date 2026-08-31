package ui

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// selectionToUI copies one committed catalog selection into the UI domain.
func selectionToUI(selection model.Selection) domainui.ModelSelection {
	return domainui.ModelSelection{
		ProviderID: string(selection.Provider), ModelID: string(selection.Model),
		ReasoningChoice: reasoningChoiceToUI(selection.ReasoningChoice),
	}
}

// reasoningChoiceToUI maps the closed model reasoning-choice set into the UI domain.
func reasoningChoiceToUI(choice model.ReasoningChoice) domainui.ReasoningChoice {
	switch choice {
	case model.ReasoningChoiceOff:
		return domainui.ReasoningChoiceOff
	case model.ReasoningChoiceOn:
		return domainui.ReasoningChoiceOn
	case model.ReasoningChoiceMinimal:
		return domainui.ReasoningChoiceMinimal
	case model.ReasoningChoiceLow:
		return domainui.ReasoningChoiceLow
	case model.ReasoningChoiceMedium:
		return domainui.ReasoningChoiceMedium
	case model.ReasoningChoiceHigh:
		return domainui.ReasoningChoiceHigh
	case model.ReasoningChoiceXHigh:
		return domainui.ReasoningChoiceXHigh
	case model.ReasoningChoiceMax:
		return domainui.ReasoningChoiceMax
	default:
		return 0
	}
}

// reasoningChoiceFromUI maps a validated UI reasoning-choice command into the model domain.
func reasoningChoiceFromUI(choice domainui.ReasoningChoice) (model.ReasoningChoice, bool) {
	switch choice {
	case domainui.ReasoningChoiceOff:
		return model.ReasoningChoiceOff, true
	case domainui.ReasoningChoiceOn:
		return model.ReasoningChoiceOn, true
	case domainui.ReasoningChoiceMinimal:
		return model.ReasoningChoiceMinimal, true
	case domainui.ReasoningChoiceLow:
		return model.ReasoningChoiceLow, true
	case domainui.ReasoningChoiceMedium:
		return model.ReasoningChoiceMedium, true
	case domainui.ReasoningChoiceHigh:
		return model.ReasoningChoiceHigh, true
	case domainui.ReasoningChoiceXHigh:
		return model.ReasoningChoiceXHigh, true
	case domainui.ReasoningChoiceMax:
		return model.ReasoningChoiceMax, true
	default:
		return "", false
	}
}
