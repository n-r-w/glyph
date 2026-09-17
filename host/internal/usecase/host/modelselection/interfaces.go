// Package modelselection owns shared active-model selection admission and commit ordering.
package modelselection

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=modelselection
//go:generate go tool mockgen -destination=operation_mock_integration_test.go -package=modelselection github.com/n-r-w/glyph/internal/operation Delivery

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// CatalogFailure exposes the stable category returned by catalog selection validation.
type CatalogFailure interface {
	error
	// CatalogSelectionCode returns the provider catalog failure category.
	CatalogSelectionCode() string
}

// Catalog resolves, validates, and atomically commits complete model selections.
type Catalog interface {
	// ResolveModel resolves a provider and model without credential I/O or state mutation.
	ResolveModel(model.ProviderID, model.ID) (model.Selection, error)
	// ResolveReasoning resolves a reasoning choice against the active model without state mutation.
	ResolveReasoning(model.ReasoningChoice) (model.Selection, error)
	// ValidateSelection validates the final target, including credentials, without state mutation.
	ValidateSelection(context.Context, model.Selection) error
	// CommitSelection atomically replaces the complete active selection.
	CommitSelection(context.Context, model.Selection) (model.Selection, model.Selection, error)
}

// Publisher enqueues one authoritative full-selection connection event.
type Publisher interface {
	// PublishSelection enqueues committed state and returns its delivery acknowledgement.
	PublishSelection(model.Selection) (func(context.Context) error, error)
}

// Runtime invokes registered selection handlers through their owning runtime instances.
type Runtime interface {
	// HandlerRuntimeAvailable reports whether the selected extension runtime can accept work.
	HandlerRuntimeAvailable(extensionID string) bool
	// HandleSelection invokes one selected handler and reports runtime loss separately from an ordinary error.
	HandleSelection(
		context.Context,
		string,
		string,
		HandlerInvocation,
	) (HandlerAction, bool, error)
}

// ContextIssuer creates a current session-bound context for a handler invocation.
type ContextIssuer interface {
	// IssueContext issues one trusted context for the available extension runtime.
	IssueContext(extensionID string) (extension.Context, error)
}

// Binding identifies the issued extension runtime-to-session context that must remain current through commit.
type Binding struct {
	// ExtensionID identifies the extension that owns the issued context.
	ExtensionID string
	// RuntimeID identifies the exact runtime incarnation.
	RuntimeID string
	// Context identifies the issued context and durable session binding.
	Context extension.ContextRef
}

// BindingFailure exposes the stable category returned by context protection.
type BindingFailure interface {
	error
	// ContextCode returns the binding failure category.
	ContextCode() string
}

// BindingProtection protects an extension binding only during final commit and publication enqueue.
type BindingProtection interface {
	// ProtectSelectionCommit validates and protects session before runtime while commit runs.
	ProtectSelectionCommit(context.Context, Binding, func() error) error
}

// IssueDelivery publishes nonfatal handler diagnostics before selection can commit.
type IssueDelivery interface {
	// DeliverExtensionIssue attempts ordered delivery to the connected Glyph client.
	DeliverSelectionIssue(context.Context, Issue) error
}

// Observer delivers committed selection changes to registered extension observers.
type Observer interface {
	// ObserveSelection invokes one observer group and returns ordered post-commit diagnostics.
	ObserveSelection(context.Context, ObservationKind, SelectionChange) []Issue
}
