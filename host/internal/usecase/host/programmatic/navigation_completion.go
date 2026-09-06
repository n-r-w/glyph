package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
)

// publicResult projects domain navigation facts into the public Programmatic completion.
func (result NavigationCompletion) publicResult() (controller.TreeNavigationResult, error) {
	issues := lo.Map(result.Issues, func(issue NavigationIssue, _ int) controller.OperationIssue {
		return controller.OperationIssue{
			Code: controller.OperationIssueCode(issue.Kind), ExtensionID: issue.ExtensionID,
			HandlerID: issue.HandlerID, Message: issue.Text,
		}
	})
	completion := controller.TreeNavigationResult{
		Status:    controller.TreeNavigationStatusCanceled,
		Committed: mo.None[controller.TreeNavigationCommitted](), Issues: issues,
	}
	if committed, present := result.Committed.Get(); present {
		mapped, err := committed.publicCommit()
		if err != nil {
			return controller.TreeNavigationResult{}, err
		}
		completion.Status = controller.TreeNavigationStatusCommitted
		completion.Committed = mo.Some(mapped)
	}
	return completion, nil
}

// publicCommit validates the created summary and projects one committed Programmatic result.
func (committed NavigationCommit) publicCommit() (controller.TreeNavigationCommitted, error) {
	summary := mo.None[controller.SessionTreeEntry]()
	if entry, present := committed.CreatedSummary.Get(); present {
		mapped, err := controller.ProjectSessionTreeEntry(entry, "")
		if err != nil {
			return controller.TreeNavigationCommitted{}, fmt.Errorf("map created branch summary: %w", err)
		}
		if mapped.BranchSummary.IsNone() {
			return controller.TreeNavigationCommitted{}, errors.New(
				"map created branch summary: summary payload is required",
			)
		}
		summary = mo.Some(mapped)
	}
	return controller.TreeNavigationCommitted{
		DestinationID: committed.DestinationID, ActiveLeafID: committed.ActiveLeafID,
		CreatedSummary: summary, NextInput: committed.NextInput,
	}, nil
}
