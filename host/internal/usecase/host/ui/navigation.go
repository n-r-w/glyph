package ui

import (
	"context"

	"github.com/samber/mo"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=navigation.go -destination=navigation_mock.go -package=ui

// NavigationIntent contains only the client request fields needed for tree navigation.
type NavigationIntent struct {
	// TargetEntryID identifies the selected tree entry.
	TargetEntryID string
	// SummaryMode selects the client-requested summary behavior.
	SummaryMode controllerui.SummaryMode
	// CustomFocus contains the optional caller-supplied summary focus.
	CustomFocus mo.Option[string]
}

// Navigator owns handler policy and atomically commits client navigation.
type Navigator interface {
	// NavigateUI publishes the committed tree before post-commit observers.
	NavigateUI(context.Context, NavigationIntent, func(session.Tree) error) (NavigationCompletion, error)
}

// NavigationFailure supplies the source-owned category without replacing its text.
type NavigationFailure interface {
	error
	// NavigationCode identifies the navigation failure category.
	NavigationCode() string
}

// NavigationCompletion contains committed domain facts or a state-free cancellation.
type NavigationCompletion struct {
	// Committed is absent when handlers canceled navigation before commit.
	Committed mo.Option[NavigationCommit]
	// Issues contains nonterminal navigation failures in occurrence order.
	Issues []NavigationIssue
}

// NavigationCommit contains metadata from one immutable navigation commit.
type NavigationCommit struct {
	// DestinationID identifies the selected navigation destination.
	DestinationID mo.Option[string]
	// ActiveLeafID identifies the resulting active tree leaf.
	ActiveLeafID mo.Option[string]
	// CreatedSummary contains the created domain entry before public projection.
	CreatedSummary mo.Option[session.Entry]
	// NextInput contains exact editable text from a selected user entry.
	NextInput mo.Option[string]
}

// NavigationIssueKind identifies a nonterminal navigation failure.
type NavigationIssueKind uint8

const (
	// NavigationHandlerError reports an ordinary request or result handler error.
	NavigationHandlerError NavigationIssueKind = iota + 1
	// NavigationInvalidHandlerAction reports rejected handler changes.
	NavigationInvalidHandlerAction
	// NavigationObserverError reports a post-commit observer failure.
	NavigationObserverError
	// NavigationDeliveryFailed reports failed publication of committed state.
	NavigationDeliveryFailed
)

// NavigationIssue preserves a capability issue before public result projection.
type NavigationIssue struct {
	// Kind identifies the source issue category.
	Kind NavigationIssueKind
	// ExtensionID identifies the owning extension when present.
	ExtensionID string
	// HandlerID identifies the registered handler when present.
	HandlerID string
	// Text contains the complete received diagnostic.
	Text string
}
