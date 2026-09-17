package programmatic

import controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"

// ModelSelectionIssueKind identifies one successful selection diagnostic.
type ModelSelectionIssueKind uint8

const (
	// ModelSelectionIssueHandlerError reports one ordinary handler failure.
	ModelSelectionIssueHandlerError ModelSelectionIssueKind = iota + 1
	// ModelSelectionIssueInvalidHandlerAction reports one malformed handler action.
	ModelSelectionIssueInvalidHandlerAction
	// ModelSelectionIssueDeliveryFailed reports failed publication after commit.
	ModelSelectionIssueDeliveryFailed
	// ModelSelectionIssueObserverError reports one ordinary observer failure.
	ModelSelectionIssueObserverError
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

// projectModelSelectionIssues maps consumer-owned diagnostics to the Programmatic controller result.
func projectModelSelectionIssues(issues []ModelSelectionIssue) []controller.OperationIssue {
	if len(issues) == 0 {
		return nil
	}
	mapped := make([]controller.OperationIssue, len(issues))
	for index, issue := range issues {
		code := controller.OperationIssueHandlerError
		switch issue.Kind {
		case ModelSelectionIssueHandlerError:
		case ModelSelectionIssueInvalidHandlerAction:
			code = controller.OperationIssueInvalidHandlerAction
		case ModelSelectionIssueDeliveryFailed:
			code = controller.OperationIssueDeliveryFailed
		case ModelSelectionIssueObserverError:
			code = controller.OperationIssueObserverError
		default:
		}
		mapped[index] = controller.OperationIssue{
			Code: code, ExtensionID: issue.ExtensionID, HandlerID: issue.HandlerID, Message: issue.Message,
		}
	}
	return mapped
}
