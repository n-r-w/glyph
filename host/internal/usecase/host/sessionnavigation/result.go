// Package sessionnavigation defines client-neutral session-tree navigation results.
package sessionnavigation

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// OperationIssueCode classifies a nonterminal extension issue without replacing its text.
type OperationIssueCode uint8

const (
	// OperationIssueHandlerError reports an ordinary request or result handler error.
	OperationIssueHandlerError OperationIssueCode = iota + 1
	// OperationIssueInvalidHandlerAction reports an invalid request or result action.
	OperationIssueInvalidHandlerAction
	// OperationIssueObserverError reports a failed post-commit observer.
	OperationIssueObserverError
	// OperationIssueDeliveryFailed reports failed publication after a navigation commit.
	OperationIssueDeliveryFailed
)

// OperationIssue reports handler identity and the complete received failure text.
type OperationIssue struct {
	// Code identifies the issue class.
	Code OperationIssueCode
	// ExtensionID identifies the owning extension when applicable.
	ExtensionID string
	// HandlerID identifies the registered handler when applicable.
	HandlerID string
	// Message preserves the received failure and all context already added to it.
	Message string
}

// Progress contains the committed state published before post-commit observers.
type Progress struct {
	// Tree is the complete committed active-session tree.
	Tree session.Tree
	// ActiveBranch contains committed active-branch entries in root-first order.
	ActiveBranch []session.Entry
}

// Result contains terminal navigation metadata or a canceled outcome.
type Result struct {
	// Canceled reports that a handler stopped navigation before commit.
	Canceled bool
	// DestinationID identifies the committed navigation destination.
	DestinationID mo.Option[string]
	// ActiveLeafID identifies the navigation commit's active leaf.
	ActiveLeafID mo.Option[string]
	// CreatedSummary contains summary metadata created by the navigation commit.
	CreatedSummary mo.Option[session.Entry]
	// NextInput contains exact selected user text when present.
	NextInput mo.Option[string]
	// Issues contains nonterminal failures in occurrence order.
	Issues []OperationIssue
}
