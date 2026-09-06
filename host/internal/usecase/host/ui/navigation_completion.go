package ui

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// frame projects domain commit facts into one UI operation completion.
func (result NavigationCompletion) frame() (controllerui.Frame, error) {
	issues := lo.Map(result.Issues, func(issue NavigationIssue, _ int) controllerui.OperationIssue {
		return controllerui.OperationIssue{
			Code: controllerui.OperationIssueCode(issue.Kind), ExtensionID: issue.ExtensionID,
			HandlerID: issue.HandlerID, Message: issue.Text,
		}
	})
	completion := controllerui.TreeNavigationResult{
		Status:    controllerui.TreeNavigationStatusCanceled,
		Committed: mo.None[controllerui.TreeNavigationCommitted](), Issues: issues,
	}
	if committed, present := result.Committed.Get(); present {
		summary, err := committed.publicSummary()
		if err != nil {
			return controllerui.Frame{}, err
		}
		completion.Status = controllerui.TreeNavigationStatusCommitted
		completion.Committed = mo.Some(controllerui.TreeNavigationCommitted{
			DestinationID: committed.DestinationID, ActiveLeafID: committed.ActiveLeafID,
			CreatedSummary: summary, NextInput: committed.NextInput,
		})
	}
	frame := controllerui.NewFrame(controllerui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(completion)
	return frame, nil
}

// publicSummary validates the created summary's public UI payload without reading newer session state.
func (committed NavigationCommit) publicSummary() (mo.Option[controllerui.SessionTreeEntry], error) {
	entry, present := committed.CreatedSummary.Get()
	if !present {
		return mo.None[controllerui.SessionTreeEntry](), nil
	}
	mapped, err := controllerui.ProjectSessionTreeEntry(entry, "")
	if err != nil {
		return mo.None[controllerui.SessionTreeEntry](), fmt.Errorf("map created branch summary: %w", err)
	}
	if mapped.BranchSummary.IsNone() {
		return mo.None[controllerui.SessionTreeEntry](), errors.New(
			"map created branch summary: summary payload is required",
		)
	}
	return mo.Some(mapped), nil
}
