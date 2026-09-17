package modelselection

import (
	"context"
	"errors"
	"fmt"

	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// uiPrepared projects one private prepared operation to the UI-owned boundary.
type uiPrepared struct {
	// prepared owns shared admission and private execution.
	prepared *preparedSelection
}

var _ hostui.PreparedModelSelection = (*uiPrepared)(nil)

// programmaticPrepared projects one private prepared operation to the Programmatic-owned boundary.
type programmaticPrepared struct {
	// prepared owns shared admission and private execution.
	prepared *preparedSelection
}

var _ hostprogrammatic.PreparedModelSelection = (*programmaticPrepared)(nil)

// PrepareUISelection projects one UI-owned command into the private selection operation.
func (s *Service) PrepareUISelection(
	command hostui.ModelSelectionCommand,
) (hostui.PreparedModelSelection, error) {
	var prepared *preparedSelection
	var err error
	switch command.Kind {
	case hostui.ModelSelectionCommandModel:
		prepared, err = s.prepareModel(command.Provider, command.Model)
	case hostui.ModelSelectionCommandReasoning:
		prepared, err = s.prepareReasoning(command.ReasoningChoice)
	default:
		return nil, fmt.Errorf("unsupported UI model selection command %d", command.Kind)
	}
	if err != nil {
		return nil, err
	}
	return &uiPrepared{prepared: prepared}, nil
}

// PrepareProgrammaticSelection projects one Programmatic-owned command into the private selection operation.
func (s *Service) PrepareProgrammaticSelection(
	command hostprogrammatic.ModelSelectionCommand,
) (hostprogrammatic.PreparedModelSelection, error) {
	var prepared *preparedSelection
	var err error
	switch command.Kind {
	case hostprogrammatic.ModelSelectionCommandModel:
		prepared, err = s.prepareModel(command.Provider, command.Model)
	case hostprogrammatic.ModelSelectionCommandReasoning:
		prepared, err = s.prepareReasoning(command.ReasoningChoice)
	default:
		return nil, fmt.Errorf("unsupported Programmatic model selection command %d", command.Kind)
	}
	if err != nil {
		return nil, err
	}
	return &programmaticPrepared{prepared: prepared}, nil
}

// Run executes the private operation and returns an explicit UI-owned result.
func (p *uiPrepared) Run(ctx context.Context) hostui.ModelSelectionResult {
	result := p.prepared.Run(ctx)
	return hostui.ModelSelectionResult{
		Selection: result.selection,
		Committed: result.committed,
		Issues:    mapUIIssues(result),
		Source:    result.source(),
	}
}

// Release frees shared selection admission.
func (p *uiPrepared) Release() { p.prepared.Release() }

// Run executes the private operation and returns an explicit Programmatic-owned result.
func (p *programmaticPrepared) Run(ctx context.Context) hostprogrammatic.ModelSelectionResult {
	result := p.prepared.Run(ctx)
	return hostprogrammatic.ModelSelectionResult{
		Selection: result.selection,
		Committed: result.committed,
		Issues:    mapProgrammaticIssues(result),
		Source:    result.source(),
	}
}

// Release frees shared selection admission.
func (p *programmaticPrepared) Release() { p.prepared.Release() }

// source returns the terminal source independently from explicit successful diagnostics.
func (r selectionResult) source() error {
	if !r.committed {
		return r.err
	}
	causes := make([]error, 0, len(r.issues)+1)
	deliveryAdded := false
	for _, issue := range r.issues {
		if issue.Code == IssueCodeObserverError && r.deliveryErr != nil && !deliveryAdded {
			causes = append(causes, r.deliveryErr)
			deliveryAdded = true
		}
		causes = append(causes, issue.Err)
	}
	if !deliveryAdded {
		causes = append(causes, r.deliveryErr)
	}
	return errors.Join(causes...)
}

// projectedIssue is the consumer-neutral value used only while projecting private results.
type projectedIssue struct {
	// code identifies the diagnostic category.
	code string
	// extensionID identifies the extension when the issue came from a handler.
	extensionID string
	// handlerID identifies the handler when the issue came from a handler.
	handlerID string
	// message contains the complete public diagnostic text.
	message string
}

// projectIssues converts private causes to detached consumer-neutral values once.
func projectIssues(result selectionResult) []projectedIssue {
	if !result.committed {
		return nil
	}
	projected := make([]projectedIssue, 0, len(result.issues)+1)
	deliveryAdded := false
	for _, issue := range result.issues {
		if issue.Code == IssueCodeObserverError && result.deliveryErr != nil && !deliveryAdded {
			projected = append(projected, projectedIssue{
				code: IssueCodeDeliveryFailed, extensionID: "", handlerID: "", message: result.deliveryErr.Error(),
			})
			deliveryAdded = true
		}
		projected = append(projected, projectedIssue{
			code: issue.Code, extensionID: issue.ExtensionID, handlerID: issue.HandlerID, message: issue.Err.Error(),
		})
	}
	if result.deliveryErr != nil && !deliveryAdded {
		projected = append(projected, projectedIssue{
			code: IssueCodeDeliveryFailed, extensionID: "", handlerID: "", message: result.deliveryErr.Error(),
		})
	}
	return projected
}

// mapUIIssues projects ordered diagnostics directly into the UI-owned result.
func mapUIIssues(result selectionResult) []hostui.ModelSelectionIssue {
	projected := projectIssues(result)
	if len(projected) == 0 {
		return nil
	}
	mapped := make([]hostui.ModelSelectionIssue, 0, len(projected))
	for _, issue := range projected {
		kind := hostui.ModelSelectionIssueHandlerError
		switch issue.code {
		case IssueCodeInvalidHandlerAction:
			kind = hostui.ModelSelectionIssueInvalidHandlerAction
		case IssueCodeDeliveryFailed:
			kind = hostui.ModelSelectionIssueDeliveryFailed
		case IssueCodeObserverError:
			kind = hostui.ModelSelectionIssueObserverError
		case IssueCodeHandlerError:
		}
		mapped = append(mapped, hostui.ModelSelectionIssue{
			Kind: kind, ExtensionID: issue.extensionID, HandlerID: issue.handlerID, Message: issue.message,
		})
	}
	return mapped
}

// mapProgrammaticIssues projects ordered diagnostics directly into the Programmatic-owned result.
func mapProgrammaticIssues(result selectionResult) []hostprogrammatic.ModelSelectionIssue {
	projected := projectIssues(result)
	if len(projected) == 0 {
		return nil
	}
	mapped := make([]hostprogrammatic.ModelSelectionIssue, 0, len(projected))
	for _, issue := range projected {
		kind := hostprogrammatic.ModelSelectionIssueHandlerError
		switch issue.code {
		case IssueCodeInvalidHandlerAction:
			kind = hostprogrammatic.ModelSelectionIssueInvalidHandlerAction
		case IssueCodeDeliveryFailed:
			kind = hostprogrammatic.ModelSelectionIssueDeliveryFailed
		case IssueCodeObserverError:
			kind = hostprogrammatic.ModelSelectionIssueObserverError
		case IssueCodeHandlerError:
		}
		mapped = append(mapped, hostprogrammatic.ModelSelectionIssue{
			Kind: kind, ExtensionID: issue.extensionID, HandlerID: issue.handlerID, Message: issue.message,
		})
	}
	return mapped
}
