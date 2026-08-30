package ui

import (
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// summaryModeFromUI maps the closed UI contract to internal navigation behavior.
func summaryModeFromUI(mode domainui.SummaryMode) (sessionnavigation.SummaryMode, bool) {
	switch mode {
	case domainui.SummaryModeNoSummary:
		return sessionnavigation.SummaryModeNoSummary, true
	case domainui.SummaryModeSummarize:
		return sessionnavigation.SummaryModeSummarize, true
	case domainui.SummaryModeSummarizeWithCustomPrompt:
		return sessionnavigation.SummaryModeSummarizeWithCustomPrompt, true
	default:
		return sessionnavigation.SummaryModeNoSummary, false
	}
}
