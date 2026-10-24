//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestInvocationFailureJoinsChildren checks retained causes, concurrent isolation and once-only release.
func TestInvocationFailureJoinsChildren(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange concurrent invocations and two accepted children under the first parent.
		runtime := &Runtime{}
		parent, finish := runtime.beginInvocation(t.Context(), "first")
		other, finishOther := runtime.beginInvocation(t.Context(), "second")
		first, err := runtime.BeginInvocationChild("first")
		require.NoError(t, err)
		second, err := runtime.BeginInvocationChild("first")
		require.NoError(t, err)
		linked, unlink := second.Link(t.Context())
		defer unlink()
		cause := errors.New("nested handler: complete original cause")
		// Act by settling an independent failure, then beginning parent teardown before the sibling releases.
		first.Fail(cause)
		first.Fail(errors.New("duplicate release must be ignored"))
		first.Release()
		first.Release()
		finished := make(chan error, 1)
		go func() { finished <- finish(context.Canceled) }()
		synctest.Wait()
		// Assert admission closes, sibling cancellation arrives, and teardown waits for the accepted sibling.
		require.ErrorIs(t, parent.Err(), context.Canceled)
		require.ErrorIs(t, linked.Err(), context.Canceled)
		require.NoError(t, other.Err())
		_, err = runtime.BeginInvocationChild("first")
		require.ErrorContains(t, err, "parent invocation has ended")
		select {
		case <-finished:
			t.Fatal("parent returned before accepted sibling settled")
		default:
		}
		second.Release()
		result := <-finished
		require.ErrorIs(t, result, cause)
		_, fatal := errors.AsType[run.FatalOperationFailure](result)
		require.True(t, fatal)
		require.NotContains(t, result.Error(), "duplicate release")
		require.NoError(t, finishOther(nil))
		_, err = runtime.BeginInvocationChild("first")
		require.ErrorContains(t, err, "unavailable")
	})
}

// TestParentSuccessClosesAndJoinsAsyncChild verifies accepted asynchronous work cannot outlive parent settlement.
func TestParentSuccessClosesAndJoinsAsyncChild(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange one child that has been admitted but has not finished.
		runtime := &Runtime{}
		_, finish := runtime.beginInvocation(t.Context(), "parent")
		child, err := runtime.BeginInvocationChild("parent")
		require.NoError(t, err)
		linked, unlink := child.Link(t.Context())
		defer unlink()
		finished := make(chan error, 1)
		// Act by returning success while the asynchronous child is still accepted.
		go func() { finished <- finish(nil) }()
		synctest.Wait()
		// Assert parent closure cancels and joins the child before returning success.
		require.ErrorIs(t, linked.Err(), context.Canceled)
		select {
		case <-finished:
			t.Fatal("parent success escaped its accepted child")
		default:
		}
		child.Release()
		require.NoError(t, <-finished)
	})
}

// TestInvocationLinkObservesCancellationBeforeRun checks cancellation before child execution is synchronous.
func TestInvocationLinkObservesCancellationBeforeRun(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange an admitted child whose owning invocation has already been canceled.
		runtime := &Runtime{}
		owner, cancel := context.WithCancel(t.Context())
		_, finish := runtime.beginInvocation(owner, "parent")
		child, err := runtime.BeginInvocationChild("parent")
		require.NoError(t, err)
		cancel()
		// Act by linking a different caller context only after parent cancellation.
		linked, unlink := child.Link(t.Context())
		defer unlink()
		defer child.Release()
		// Assert no child action can start with a live context after its parent has ended.
		require.ErrorIs(t, linked.Err(), context.Canceled)
		child.Release()
		require.ErrorIs(t, finish(context.Canceled), context.Canceled)
	})
}

// TestInvocationGenerationIsolation checks connection-local identifiers and pure owning cancellation.
func TestInvocationGenerationIsolation(t *testing.T) {
	t.Parallel()
	// Arrange two independent runtime connections reusing the same envelope identifier.
	first := &Runtime{}
	second := &Runtime{}
	ctx, cancel := context.WithCancel(t.Context())
	_, finishFirst := first.beginInvocation(ctx, "same")
	other, finishSecond := second.beginInvocation(t.Context(), "same")
	child, err := first.BeginInvocationChild("same")
	require.NoError(t, err)
	// Act through pure owning cancellation and once-only child cleanup.
	cancel()
	child.Release()
	child.Release()
	result := finishFirst(context.Canceled)
	// Assert pure cancellation remains nonfatal and the other generation remains independently usable.
	require.ErrorIs(t, result, context.Canceled)
	_, fatal := errors.AsType[run.FatalOperationFailure](result)
	require.False(t, fatal)
	require.NoError(t, other.Err())
	otherChild, err := second.BeginInvocationChild("same")
	require.NoError(t, err)
	otherChild.Release()
	require.NoError(t, finishSecond(nil))
}
