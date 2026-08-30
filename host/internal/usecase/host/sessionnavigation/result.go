// Package sessionnavigation defines client-neutral session-tree navigation results.
package sessionnavigation

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// OperationIssueCode identifies one safe nonterminal extension issue.
type OperationIssueCode uint8

const (
	// OperationIssueHandlerError reports an ordinary request or result handler error.
	OperationIssueHandlerError OperationIssueCode = iota + 1
	// OperationIssueInvalidHandlerAction reports an invalid request or result action.
	OperationIssueInvalidHandlerAction
	// OperationIssueObserverError reports a failed post-commit observer.
	OperationIssueObserverError
)

// OperationIssue reports one safe ordered handler or observer issue.
type OperationIssue struct {
	// Code identifies the issue class.
	Code OperationIssueCode
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// HandlerID identifies the registered handler.
	HandlerID string
	// Message contains a safe Host-owned description.
	Message string
}

// Result contains one committed navigation state or canceled outcome.
type Result struct {
	// Canceled reports that a handler stopped navigation before commit.
	Canceled bool
	// Tree is the complete committed active-session tree.
	Tree session.Tree
	// ActiveLeafID identifies the committed destination or is absent for the implicit root.
	ActiveLeafID mo.Option[string]
	// ActiveBranch contains committed active-branch entries in root-first order.
	ActiveBranch []session.Entry
	// NextInput contains exact selected user text when the navigation target is a user message.
	NextInput mo.Option[string]
	// Issues contains safe nonterminal extension issues in occurrence order.
	Issues []OperationIssue
}
