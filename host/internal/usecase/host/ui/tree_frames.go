package ui

import (
	"errors"
	"fmt"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/internal/operation"
)

// sessionTreeFrame projects the complete tree without private extension payload bytes.
func sessionTreeFrame(tree session.Tree) (controllerui.Frame, error) {
	mapped, err := mapSessionTree(tree)
	if err != nil {
		return controllerui.Frame{}, err
	}
	frame := controllerui.NewFrame(controllerui.FrameSessionTree)
	frame.SessionTree = mo.Some(mapped)
	return frame, nil
}

// navigationProgressCallback maps and enqueues committed navigation state.
func navigationProgressCallback(
	reporter operation.Reporter[controllerui.Frame],
) func(sessionnavigation.Progress) error {
	return func(progress sessionnavigation.Progress) error {
		tree, err := mapSessionTree(progress.Tree)
		if err != nil {
			return err
		}
		branch, err := mapSessionEntries(progress.ActiveBranch)
		if err != nil {
			return fmt.Errorf("map active branch: %w", err)
		}
		frame := controllerui.NewFrame(controllerui.FrameSessionTreeNavigationProgress)
		frame.TreeNavigationProgress = mo.Some(controllerui.TreeNavigationProgress{Tree: tree, ActiveBranch: branch})
		return reporter.Report(frame)
	}
}

// navigationFrame projects terminal navigation metadata.
func navigationFrame(result sessionnavigation.Result) (controllerui.Frame, error) {
	if result.Canceled {
		return canceledNavigationFrame(mapOperationIssues(result.Issues)), nil
	}
	createdSummary := mo.None[controllerui.SessionTreeEntry]()
	if entry, present := result.CreatedSummary.Get(); present {
		mapped, err := controllerui.ProjectSessionTreeEntry(entry, "")
		if err != nil {
			return controllerui.Frame{}, fmt.Errorf("map created branch summary: %w", err)
		}
		if mapped.BranchSummary.IsNone() {
			return controllerui.Frame{}, errors.New("map created branch summary: summary payload is required")
		}
		createdSummary = mo.Some(mapped)
	}
	frame := controllerui.NewFrame(controllerui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(controllerui.TreeNavigationResult{
		Status: controllerui.TreeNavigationStatusCommitted,
		Committed: mo.Some(controllerui.TreeNavigationCommitted{
			DestinationID:  result.DestinationID,
			ActiveLeafID:   result.ActiveLeafID,
			CreatedSummary: createdSummary,
			NextInput:      result.NextInput,
		}),
		Issues: mapOperationIssues(result.Issues),
	})
	return frame, nil
}

// canceledNavigationFrame reports cancellation without speculative state.
func canceledNavigationFrame(issues []controllerui.OperationIssue) controllerui.Frame {
	frame := controllerui.NewFrame(controllerui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(controllerui.TreeNavigationResult{
		Status:    controllerui.TreeNavigationStatusCanceled,
		Committed: mo.None[controllerui.TreeNavigationCommitted](), Issues: issues,
	})
	return frame
}

// mapSessionTree projects every tree entry in persistence order.
func mapSessionTree(tree session.Tree) (controllerui.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped, err := lo.MapErr(entries, func(entry session.Entry, index int) (controllerui.SessionTreeEntry, error) {
		result, mapErr := controllerui.ProjectSessionTreeEntry(entry, labels[entry.ID])
		if mapErr != nil {
			return controllerui.SessionTreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
		}
		return result, nil
	})
	if err != nil {
		return controllerui.SessionTree{}, err
	}
	return controllerui.SessionTree{Entries: mapped, ActiveLeafID: tree.ActiveLeafID()}, nil
}

// mapOperationIssues projects safe ordered navigation issues for UI delivery.
func mapOperationIssues(issues []sessionnavigation.OperationIssue) []controllerui.OperationIssue {
	return lo.Map(issues, func(issue sessionnavigation.OperationIssue, _ int) controllerui.OperationIssue {
		return controllerui.OperationIssue{
			Code: controllerui.OperationIssueCode(issue.Code), ExtensionID: issue.ExtensionID,
			HandlerID: issue.HandlerID, Message: issue.Message,
		}
	})
}
