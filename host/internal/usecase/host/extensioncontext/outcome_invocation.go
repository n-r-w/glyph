package extensioncontext

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// OutcomeInvocation reports one terminal compaction outcome.
type OutcomeInvocation struct {
	// State contains acquired original and last valid composition values.
	State mo.Option[PolicyState]
	// ParentCompactionID correlates observer children with their owning lifetime.
	ParentCompactionID mo.Option[string]
	// Committed contains durable state when persistence succeeded.
	Committed mo.Option[session.Entry]
	// Canceled reports explicit handler cancellation.
	Canceled bool
	// Error contains complete terminal failure text when present.
	Error mo.Option[string]
}
