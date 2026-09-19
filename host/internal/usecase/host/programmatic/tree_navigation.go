package programmatic

import (
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// programmaticNavigationCallback maps and reports committed navigation state.
func programmaticNavigationCallback(
	reporter operation.Reporter[programmatic.OperationProgress],
) func(session.Tree) error {
	return func(progress session.Tree) error {
		mapped, err := mapTreeNavigationProgress(progress)
		if err != nil {
			return err
		}
		return reporter.Report(programmatic.OperationProgress{
			AgentEvent: mo.None[programmatic.AgentEvent](), TreeNavigation: mo.Some(mapped),
			TreeNavigationRetry: mo.None[programmatic.RetryProgress](),
		})
	}
}

// programmaticNavigationRetryCallback reports branch-summary retries on the owning operation.
func programmaticNavigationRetryCallback(
	reporter operation.Reporter[programmatic.OperationProgress],
) func(int64, int64, time.Duration, string) error {
	return func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error {
		return reporter.Report(programmatic.OperationProgress{
			AgentEvent:     mo.None[programmatic.AgentEvent](),
			TreeNavigation: mo.None[programmatic.TreeNavigationProgress](),
			TreeNavigationRetry: mo.Some(programmatic.RetryProgress{
				CompletedAttempts: completedAttempts, AttemptLimit: attemptLimit, Delay: delay, Error: failure,
			}),
		})
	}
}

// validSummaryMode checks the closed client summary-mode contract before navigation admission.
func validSummaryMode(mode programmatic.SummaryMode) bool {
	switch mode {
	case programmatic.SummaryModeNoSummary, programmatic.SummaryModeSummarize,
		programmatic.SummaryModeSummarizeWithCustomPrompt:
		return true
	default:
		return false
	}
}
