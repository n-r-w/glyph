package programmatic

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/internal/operation"
)

// programmaticNavigationCallback maps and reports committed navigation state.
func programmaticNavigationCallback(
	reporter operation.Reporter[programmatic.OperationProgress],
) func(sessionnavigation.Progress) error {
	return func(progress sessionnavigation.Progress) error {
		mapped, err := mapTreeNavigationProgress(progress)
		if err != nil {
			return err
		}
		return reporter.Report(programmatic.OperationProgress{
			AgentEvent:     mo.None[programmatic.AgentEvent](),
			TreeNavigation: mo.Some(mapped),
		})
	}
}

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
