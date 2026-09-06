package sessiontree

import (
	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// uiCompletion selects committed facts for the UI consumer before public projection.
func (result navigationResult) uiCompletion() ui.NavigationCompletion {
	committed := mo.None[ui.NavigationCommit]()
	if !result.Canceled {
		committed = mo.Some(ui.NavigationCommit{
			DestinationID: result.DestinationID, ActiveLeafID: result.ActiveLeafID,
			CreatedSummary: result.CreatedSummary, NextInput: result.NextInput,
		})
	}
	return ui.NavigationCompletion{
		Committed: committed,
		Issues: lo.Map(result.Issues, func(issue navigationIssue, _ int) ui.NavigationIssue {
			return issue.uiIssue()
		}),
	}
}

// programmaticCompletion selects committed facts for the Programmatic consumer before public projection.
func (result navigationResult) programmaticCompletion() programmatic.NavigationCompletion {
	committed := mo.None[programmatic.NavigationCommit]()
	if !result.Canceled {
		committed = mo.Some(programmatic.NavigationCommit{
			DestinationID: result.DestinationID, ActiveLeafID: result.ActiveLeafID,
			CreatedSummary: result.CreatedSummary, NextInput: result.NextInput,
		})
	}
	return programmatic.NavigationCompletion{
		Committed: committed,
		Issues: lo.Map(result.Issues, func(issue navigationIssue, _ int) programmatic.NavigationIssue {
			return issue.programmaticIssue()
		}),
	}
}

// uiIssue classifies one capability issue for the UI consumer while preserving its complete text.
func (issue navigationIssue) uiIssue() ui.NavigationIssue {
	return ui.NavigationIssue{
		Kind: ui.NavigationIssueKind(issue.Code), ExtensionID: issue.ExtensionID,
		HandlerID: issue.HandlerID, Text: issue.Message,
	}
}

// programmaticIssue classifies one capability issue for Programmatic completion.
func (issue navigationIssue) programmaticIssue() programmatic.NavigationIssue {
	return programmatic.NavigationIssue{
		Kind: programmatic.NavigationIssueKind(issue.Code), ExtensionID: issue.ExtensionID,
		HandlerID: issue.HandlerID, Text: issue.Message,
	}
}
