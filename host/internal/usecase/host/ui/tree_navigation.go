package ui

import (
	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// summaryModeFromUI maps the closed UI contract to internal navigation behavior.
func summaryModeFromUI(mode controllerui.SummaryMode) (sessionnavigation.SummaryMode, bool) {
	switch mode {
	case controllerui.SummaryModeNoSummary:
		return sessionnavigation.SummaryModeNoSummary, true
	case controllerui.SummaryModeSummarize:
		return sessionnavigation.SummaryModeSummarize, true
	case controllerui.SummaryModeSummarizeWithCustomPrompt:
		return sessionnavigation.SummaryModeSummarizeWithCustomPrompt, true
	default:
		return sessionnavigation.SummaryModeNoSummary, false
	}
}
