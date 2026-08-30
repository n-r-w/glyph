package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// mapSessionTree projects every tree entry while removing private extension payload bytes.
func mapSessionTree(tree session.Tree) (controller.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped, err := lo.MapErr(entries, func(entry session.Entry, index int) (controller.SessionTreeEntry, error) {
		result, mapErr := mapSessionTreeEntry(entry, labels[entry.ID])
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

// mapSessionTreeEntry maps one closed tree payload without extension data.
func mapSessionTreeEntry(entry session.Entry, label string) (controller.SessionTreeEntry, error) {
	mapped := controller.SessionTreeEntry{
		ID: entry.ID, ParentID: entry.ParentID, CreatedAt: entry.CreatedAt, Label: label,
		Kind: controller.SessionTreeEntryUnspecified,
		User: mo.None[model.Message](), Model: mo.None[controller.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[controller.ToolResult](),
		Extension: mo.None[controller.ExtensionEntry](), BranchSummary: mo.None[controller.BranchSummary](),
	}
	if extension, present := entry.Extension.Get(); present {
		mapped.Kind = controller.SessionTreeEntryExtension
		mapped.Extension = mo.Some(controller.ExtensionEntry{
			ExtensionID: extension.ExtensionID, EntryType: extension.EntryType,
		})
		return mapped, nil
	}
	if summary, present := entry.BranchSummary.Get(); present {
		mapped.Kind = controller.SessionTreeEntryBranchSummary
		mapped.BranchSummary = mo.Some(controller.BranchSummary{
			Summary: summary.Summary, FirstEntryID: summary.FirstEntryID, LastEntryID: summary.LastEntryID,
			Provider: summary.Provider, Model: summary.Model, ReasoningChoice: summary.ReasoningChoice,
			Usage: summary.Usage, EstimatedCost: summary.EstimatedCost,
		})
		return mapped, nil
	}
	projected, err := mapSessionEntries([]session.Entry{entry})
	if err != nil {
		return controller.SessionTreeEntry{}, err
	}
	if len(projected) != 1 {
		return controller.SessionTreeEntry{}, errors.New("tree entry has no public payload")
	}
	public := projected[0]
	mapped.User = public.User
	mapped.Model = public.Model
	mapped.EstimatedCost = public.EstimatedCost
	mapped.ToolResult = public.ToolResult
	switch public.Kind {
	case controller.HistoryEntryUser:
		mapped.Kind = controller.SessionTreeEntryUser
	case controller.HistoryEntryModel:
		mapped.Kind = controller.SessionTreeEntryModel
	case controller.HistoryEntryToolResult:
		mapped.Kind = controller.SessionTreeEntryToolResult
	case controller.HistoryEntryUnspecified:
		return controller.SessionTreeEntry{}, errors.New("tree entry payload is unspecified")
	default:
		return controller.SessionTreeEntry{}, fmt.Errorf("unknown tree entry payload %d", public.Kind)
	}
	return mapped, nil
}

// mapTreeNavigationCommitted projects committed navigation snapshots for Programmatic Control.
func mapTreeNavigationCommitted(result sessionnavigation.Result) (controller.TreeNavigationCommitted, error) {
	tree, err := mapSessionTree(result.Tree)
	if err != nil {
		return controller.TreeNavigationCommitted{}, err
	}
	activeBranch, err := mapSessionEntries(result.ActiveBranch)
	if err != nil {
		return controller.TreeNavigationCommitted{}, fmt.Errorf("map active branch: %w", err)
	}
	return controller.TreeNavigationCommitted{
		Tree: tree, ActiveBranch: activeBranch, NextInput: result.NextInput,
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
