// Package lifecycle owns ordered extension observation of Agent Core events.
package lifecycle

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=lifecycle

// Runtime supplies availability and low-level lifecycle invocation.
type Runtime interface {
	// HandlerRuntimeAvailable reports whether one accepted extension can receive work.
	HandlerRuntimeAvailable(extensionID string) bool
	// ObserveLifecycle invokes one lifecycle observer through its owning runtime.
	ObserveLifecycle(context.Context, string, string, extension.Context, Event) (bool, error)
}

// ContextIssuer supplies the current session binding at event delivery.
type ContextIssuer interface {
	// IssueContext returns the current runtime-to-session binding.
	IssueContext(extensionID string) (extension.Context, error)
}

// IssueDelivery publishes one diagnosable nonterminal observer issue.
type IssueDelivery interface {
	// DeliverExtensionIssue attempts ordered delivery to the connected Glyph client.
	DeliverExtensionIssue(context.Context, Issue) error
}
