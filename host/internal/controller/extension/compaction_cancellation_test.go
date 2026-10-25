//go:build !integration

package extension

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestCommittedCompactionCancellation verifies typed receipts under pure owning and mixed cancellation.
func TestCommittedCompactionCancellation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		independent bool
		deadline    bool
	}{
		{name: "owning cancellation", independent: false, deadline: false},
		{name: "owning deadline", independent: false, deadline: true},
		{name: "mixed failure", independent: true, deadline: false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a committed receipt and its owning cancellation context.
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cause := context.Canceled
			if scenario.deadline {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithDeadline(t.Context(), time.Time{})
				t.Cleanup(deadlineCancel)
				cause = context.DeadlineExceeded
			}
			independent := errors.New("independent committed compaction failure")
			if scenario.independent {
				cause = errors.Join(cause, extensionPostCommitFailure{
					code: extensionPostCommitFailureCode, cause: independent,
				})
			}
			committed := session.Entry{
				ID: committedCancellationEntryID, ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
				Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
				EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
				Compaction: mo.Some(session.CompactionEntry{
					Summary:          "retained summary",
					FirstKeptEntryID: committedCancellationKeptID,
					Source: session.CompactionSource{
						ExtensionID: mo.Some(committedCancellationExtensionID),
						Requests:    nil,
					},
					Details: mo.None[[]byte](),
				}),
			}

			// Act through the Extension completion boundary.
			result, err := mapCompactionCompletion(
				ctx,
				CompactionResult{Committed: mo.Some(committed), Canceled: false},
				cause,
			)

			// Assert only mixed failure produces an error, with original identity and the actual typed receipt.
			if scenario.independent {
				require.ErrorIs(t, err, independent)
				require.Equal(t, cause.Error(), err.Error())
				failure, present := errors.AsType[*extensionsdk.FailureError](err)
				require.True(t, present)
				require.Equal(t, extensionPostCommitFailureCode, failure.Code())
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, committed.ID, result.GetCommitted().GetEntryId())
			require.Equal(t, cause.Error(), result.GetError())
		})
	}
}

const (
	// committedCancellationEntryID identifies the retained compaction marker.
	committedCancellationEntryID = "committed-cancellation-marker"
	// committedCancellationKeptID identifies the retained suffix.
	committedCancellationKeptID = "committed-cancellation-kept"
	// committedCancellationExtensionID identifies the policy producer.
	committedCancellationExtensionID = "committed-cancellation-policy"
)
