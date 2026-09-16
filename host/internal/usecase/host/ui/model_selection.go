package ui

import controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

// ModelSelectionIssueKind identifies one successful selection diagnostic.
type ModelSelectionIssueKind uint8

const (
	// ModelSelectionIssueHandlerError reports one ordinary handler failure.
	ModelSelectionIssueHandlerError ModelSelectionIssueKind = iota + 1
	// ModelSelectionIssueInvalidHandlerAction reports one malformed handler action.
	ModelSelectionIssueInvalidHandlerAction
	// ModelSelectionIssueDeliveryFailed reports failed publication after commit.
	ModelSelectionIssueDeliveryFailed
)

// ModelSelectionIssue contains one ordered successful selection diagnostic.
type ModelSelectionIssue struct {
	// Kind identifies the diagnostic category.
	Kind ModelSelectionIssueKind
	// ExtensionID identifies the extension when the issue came from a handler.
	ExtensionID string
	// HandlerID identifies the handler when the issue came from a handler.
	HandlerID string
	// Message contains the complete public diagnostic text.
	Message string
}

// projectModelSelectionIssues maps consumer-owned diagnostics to the UI controller result.
func projectModelSelectionIssues(issues []ModelSelectionIssue) []controllerui.OperationIssue {
	if len(issues) == 0 {
		return nil
	}
	mapped := make([]controllerui.OperationIssue, len(issues))
	for index, issue := range issues {
		code := controllerui.OperationIssueHandlerError
		switch issue.Kind {
		case ModelSelectionIssueHandlerError:
		case ModelSelectionIssueInvalidHandlerAction:
			code = controllerui.OperationIssueInvalidHandlerAction
		case ModelSelectionIssueDeliveryFailed:
			code = controllerui.OperationIssueDeliveryFailed
		default:
		}
		mapped[index] = controllerui.OperationIssue{
			Code: code, ExtensionID: issue.ExtensionID, HandlerID: issue.HandlerID, Message: issue.Message,
		}
	}
	return mapped
}
