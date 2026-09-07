//go:build !integration

package operation

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

// TestLateConfirmationAppliesOnlyToSentItem separates actual delivery from canceled waits and queued disposal.
func TestLateConfirmationAppliesOnlyToSentItem(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange two declared reports with only the first entering actual transport Send.
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(context.Canceled)
		source := errors.New("in-flight report source")
		queuedSource := errors.New("unsent queued report source")
		cause := errors.New("delivery wait canceled")
		started, release := make(chan struct{}), make(chan struct{})
		writer := newWriter(2, func(string) error {
			return SendWithContext(ctx, func() error {
				close(started)
				<-release
				return nil
			})
		})
		first, err := writer.EnqueueAcknowledged("first", source)
		require.NoError(t, err)
		queued, err := writer.EnqueueAcknowledged("queued", queuedSource)
		require.NoError(t, err)
		done := make(chan error, 1)

		// Act by stopping the writer before actual Send returns, then make success observable.
		go func() { done <- writer.Run(ctx) }()
		<-started
		cancel(cause)
		require.ErrorIs(t, <-done, cause)
		require.ErrorIs(t, first.Wait(t.Context()), cause)
		complete, err := first.Result()
		require.False(t, complete)
		require.NoError(t, err)
		require.ErrorIs(t, writer.SourceErrors(), source)
		close(release)
		synctest.Wait()

		// Assert final success changes confirmation only, not wait results or the unsent item's outcome.
		complete, err = first.Result()
		require.True(t, complete)
		require.NoError(t, err)
		require.ErrorIs(t, first.Wait(t.Context()), cause)
		complete, err = queued.Result()
		require.True(t, complete)
		require.ErrorIs(t, err, cause)
		require.NotErrorIs(t, writer.SourceErrors(), source)
		require.ErrorIs(t, writer.SourceErrors(), queuedSource)
	})
}
