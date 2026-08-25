package ui

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// selectionToUI copies one committed catalog selection into the UI domain.
func selectionToUI(selection model.Selection) domainui.ModelSelection {
	return domainui.ModelSelection{
		ProviderID: string(selection.Provider), ModelID: string(selection.Model),
		ReasoningLevel: reasoningLevelToUI(selection.ReasoningLevel),
	}
}

// reasoningLevelToUI maps the closed model reasoning set into the UI domain.
func reasoningLevelToUI(level model.ReasoningLevel) domainui.ReasoningLevel {
	switch level {
	case model.ReasoningLevelNone:
		return domainui.ReasoningLevelNone
	case model.ReasoningLevelMinimal:
		return domainui.ReasoningLevelMinimal
	case model.ReasoningLevelLow:
		return domainui.ReasoningLevelLow
	case model.ReasoningLevelMedium:
		return domainui.ReasoningLevelMedium
	case model.ReasoningLevelHigh:
		return domainui.ReasoningLevelHigh
	case model.ReasoningLevelXHigh:
		return domainui.ReasoningLevelXHigh
	case model.ReasoningLevelMax:
		return domainui.ReasoningLevelMax
	default:
		return 0
	}
}

// reasoningLevelFromUI maps a validated UI reasoning command into the model domain.
func reasoningLevelFromUI(level domainui.ReasoningLevel) (model.ReasoningLevel, bool) {
	switch level {
	case domainui.ReasoningLevelNone:
		return model.ReasoningLevelNone, true
	case domainui.ReasoningLevelMinimal:
		return model.ReasoningLevelMinimal, true
	case domainui.ReasoningLevelLow:
		return model.ReasoningLevelLow, true
	case domainui.ReasoningLevelMedium:
		return model.ReasoningLevelMedium, true
	case domainui.ReasoningLevelHigh:
		return model.ReasoningLevelHigh, true
	case domainui.ReasoningLevelXHigh:
		return model.ReasoningLevelXHigh, true
	case domainui.ReasoningLevelMax:
		return model.ReasoningLevelMax, true
	default:
		return "", false
	}
}
