package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// mapSessionTree projects every tree entry while removing private extension payload bytes.
func mapSessionTree(tree session.Tree) (controller.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped, err := lo.MapErr(entries, func(entry session.Entry, index int) (controller.SessionTreeEntry, error) {
		result, mapErr := controller.ProjectSessionTreeEntry(entry, labels[entry.ID])
		if mapErr != nil {
			return controller.SessionTreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
		}
		return result, nil
	})
	if err != nil {
		return controller.SessionTree{}, err
	}
	return controller.SessionTree{Entries: mapped, ActiveLeafID: tree.ActiveLeafID()}, nil
}

// mapTreeNavigationProgress projects committed navigation state for Programmatic progress.
func mapTreeNavigationProgress(progress sessionnavigation.Progress) (controller.TreeNavigationProgress, error) {
	tree, err := mapSessionTree(progress.Tree)
	if err != nil {
		return controller.TreeNavigationProgress{}, err
	}
	activeBranch, err := mapSessionEntries(progress.ActiveBranch)
	if err != nil {
		return controller.TreeNavigationProgress{}, fmt.Errorf("map active branch: %w", err)
	}
	return controller.TreeNavigationProgress{Tree: tree, ActiveBranch: activeBranch}, nil
}

// mapTreeNavigationCommitted projects terminal commit metadata for Programmatic Control.
func mapTreeNavigationCommitted(result sessionnavigation.Result) (controller.TreeNavigationCommitted, error) {
	createdSummary := mo.None[controller.SessionTreeEntry]()
	if entry, present := result.CreatedSummary.Get(); present {
		mapped, err := controller.ProjectSessionTreeEntry(entry, "")
		if err != nil {
			return controller.TreeNavigationCommitted{}, fmt.Errorf("map created branch summary: %w", err)
		}
		if mapped.BranchSummary.IsNone() {
			return controller.TreeNavigationCommitted{}, errors.New(
				"map created branch summary: summary payload is required",
			)
		}
		createdSummary = mo.Some(mapped)
	}
	return controller.TreeNavigationCommitted{
		DestinationID:  result.DestinationID,
		ActiveLeafID:   result.ActiveLeafID,
		CreatedSummary: createdSummary,
		NextInput:      result.NextInput,
	}, nil
}

// mapOperationIssues projects safe ordered navigation issues for Programmatic Control.
func mapOperationIssues(issues []sessionnavigation.OperationIssue) []controller.OperationIssue {
	return lo.Map(issues, func(issue sessionnavigation.OperationIssue, _ int) controller.OperationIssue {
		return controller.OperationIssue{
			Code: controller.OperationIssueCode(issue.Code), ExtensionID: issue.ExtensionID,
			HandlerID: issue.HandlerID, Message: issue.Message,
		}
	})
}
