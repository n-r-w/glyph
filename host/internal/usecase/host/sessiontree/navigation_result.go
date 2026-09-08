package sessiontree

import (
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// navigationIssueCode classifies a nonterminal extension issue without replacing its text.
type navigationIssueCode uint8

const (
	// navigationIssueHandlerError reports an ordinary request or result handler error.
	navigationIssueHandlerError navigationIssueCode = iota + 1
	// navigationIssueInvalidHandlerAction reports an invalid request or result action.
	navigationIssueInvalidHandlerAction
	// navigationIssueObserverError reports a failed post-commit observer.
	navigationIssueObserverError
	// navigationIssueDeliveryFailed reports failed publication after a navigation commit.
	navigationIssueDeliveryFailed
)

// navigationIssue reports handler identity and the complete received failure text.
type navigationIssue struct {
	// Code identifies the issue class.
	Code navigationIssueCode
	// ExtensionID identifies the owning extension when applicable.
	ExtensionID string
	// HandlerID identifies the registered handler when applicable.
	HandlerID string
	// Message preserves the received failure and all context already added to it.
	Message string
	// source retains the original Go cause until navigation either completes or returns an error.
	source error
}

// joinNavigationIssues retains earlier handler sources only when navigation returns a later failure.
func joinNavigationIssues(primary error, issues []navigationIssue) error {
	if primary == nil {
		return nil
	}
	for _, issue := range issues {
		primary = errors.Join(primary, issue.source)
	}
	return primary
}

// navigationResult contains terminal navigation metadata or a canceled outcome.
type navigationResult struct {
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
	Issues []navigationIssue
}
