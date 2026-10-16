// Package policy owns manual compaction composition and conversation budgeting.
package policy

import (
	"context"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock_test.go -package=policy -build_constraint=!integration

// Host supplies scoped participant and session operations.
type Host interface {
	// Request invokes a request participant.
	Request(context.Context, compaction.Participant, compaction.State) (compaction.RequestAction, error)
	// Generate invokes the selected generator.
	Generate(context.Context, compaction.Participant, compaction.State) (compaction.Result, error)
	// InvokeResultHandler invokes a result participant.
	InvokeResultHandler(
		context.Context,
		compaction.Participant,
		compaction.State,
		compaction.Result,
	) (compaction.ResultAction, error)
	// Project validates a candidate and returns its model-visible history.
	Project(context.Context, compaction.Result) ([]compaction.Entry, error)
	// Commit requests one durable replacement.
	Commit(context.Context, compaction.Result) error
}
