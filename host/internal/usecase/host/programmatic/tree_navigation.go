package programmatic

import (
	"github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// summaryModeFromProgrammatic maps the closed client contract to internal navigation behavior.
func summaryModeFromProgrammatic(mode programmatic.SummaryMode) (sessionnavigation.SummaryMode, bool) {
	switch mode {
	case programmatic.SummaryModeNoSummary:
		return sessionnavigation.SummaryModeNoSummary, true
	case programmatic.SummaryModeSummarize:
		return sessionnavigation.SummaryModeSummarize, true
	case programmatic.SummaryModeSummarizeWithCustomPrompt:
		return sessionnavigation.SummaryModeSummarizeWithCustomPrompt, true
	default:
		return sessionnavigation.SummaryModeNoSummary, false
	}
}
