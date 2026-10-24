//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

// TestInvocationFailureCancelsBeforeChildRelease checks independent failure closes admission during accepted cleanup.
func TestInvocationFailureCancelsBeforeChildRelease(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange one accepted child whose resource release remains pending.
		runtime := &Runtime{}
		owner, finish := runtime.beginInvocation(t.Context(), "parent")
		child, err := runtime.BeginInvocationChild("parent")
		require.NoError(t, err)
		cause := errors.New("independent observer failure before resource release")
		// Act by retaining failure before ending child ownership.
		child.Fail(cause)
		finished := make(chan error, 1)
		go func() { finished <- finish(context.Canceled) }()
		synctest.Wait()
		// Assert failure cancels the owner immediately while parent settlement still joins accepted cleanup.
		require.ErrorIs(t, owner.Err(), context.Canceled)
		_, err = runtime.BeginInvocationChild("parent")
		require.ErrorIs(t, err, cause)
		select {
		case <-finished:
			t.Fatal("parent settled before child resources released")
		default:
		}
		child.Release()
		require.ErrorIs(t, <-finished, cause)
	})
}

// TestChildCallerCancellationDoesNotCancelParent checks caller cancellation remains distinct from invocation ownership.
func TestChildCallerCancellationDoesNotCancelParent(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange one live parent and an independently cancelable child caller.
		runtime := &Runtime{}
		owner, finish := runtime.beginInvocation(t.Context(), "parent")
		child, err := runtime.BeginInvocationChild("parent")
		require.NoError(t, err)
		caller, cancel := context.WithCancel(t.Context())
		linked, unlink := child.Link(caller)
		defer unlink()
		// Act through caller cancellation without an independent extension failure.
		cancel()
		child.Release()
		// Assert cancellation is local to the child and ordinary parent success remains possible.
		require.ErrorIs(t, linked.Err(), context.Canceled)
		require.NoError(t, owner.Err())
		require.NoError(t, finish(nil))
	})
}
