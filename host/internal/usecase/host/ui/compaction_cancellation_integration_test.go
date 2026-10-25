//go:build integration

package ui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestCommittedCompactionTerminalCancellation verifies receipt and cause retention under owning and mixed cancellation.
func TestCommittedCompactionTerminalCancellation(t *testing.T) {
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
			// Arrange the owner context and a real mapped committed compaction result.
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
				cause = errors.Join(cause, uiIntegrationError{
					code: controllerui.FailureCodeExtensionFailed, cause: independent,
				})
			}
			committed := uiCompactionEntry()
			prepared := &preparedUIOperation{
				run: func(context.Context, operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
					return compactionCompletionFrame(ManualCompactionResult{
						Committed: mo.Some[session.Entry](committed), Canceled: false,
					}, cause)
				},
				failureCode: sessionOperationFailureCode, release: func() {}, releaseOnce: sync.Once{},
			}

			// Act through the prepared UI terminal producer.
			outcome := prepared.Run(ctx, operation.Reporter[controllerui.Frame]{})

			// Assert pure owning cancellation completes with the receipt and mixed failure remains Failed.
			expected := operation.TerminalStateCompleted
			if scenario.independent {
				expected = operation.TerminalStateFailed
				require.ErrorIs(t, outcome.Err(), independent)
				require.Equal(t, controllerui.FailureCodeExtensionFailed, outcome.Code())
			}
			require.Equal(t, expected, outcome.State())
			require.ErrorIs(t, outcome.SourceError(), cause)
			frame, present := outcome.Result()
			require.True(t, present)
			require.Len(t, frame.SessionEntries, 1)
			require.Equal(t, committed.ID, frame.SessionEntries[0].ID)
			require.Equal(t, cause.Error(), frame.CompactionError.OrEmpty())
		})
	}
}
