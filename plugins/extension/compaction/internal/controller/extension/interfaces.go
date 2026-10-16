package extension

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// Policy coordinates manual compaction and validates outgoing conversation input.
type Policy interface {
	// Compact coordinates context compaction.
	Compact(context.Context, compaction.Input) (Outcome, error)
	// ValidateOutgoing checks the captured conversation budget.
	ValidateOutgoing(context.Context, compaction.Input) error
}

// Generator produces a complete result from a composed request.
type Generator interface {
	// Generate summarizes the composed source.
	Generate(context.Context, compaction.Request) (compaction.Result, error)
}

// Outcome contains policy state and the policy's decision.
type Outcome struct {
	// State contains the state of compaction composition.
	State mo.Option[compaction.State]
	// Preserve indicates a decision to keep history without replacement.
	Preserve bool
	// Canceled indicates participant cancellation.
	Canceled bool
}
