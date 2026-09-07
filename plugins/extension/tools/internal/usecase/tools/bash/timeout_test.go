//go:build !integration

package bash

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// TestServiceExecuteTimeout preserves fractional seconds and clamps positive sub-nanosecond values.
func TestServiceExecuteTimeout(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		// name identifies the duration interpretation under test.
		name string
		// seconds contains validated controller intent.
		seconds float64
		// duration is the expected process lifetime before timeout cancellation.
		duration time.Duration
	}{
		{name: "fractional", seconds: 0.01, duration: 10 * time.Millisecond},
		{name: "sub-nanosecond", seconds: 1e-10, duration: time.Nanosecond},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a process that retains its cancellation cause and bounded output.
				runner := NewMockProcessRunner(gomock.NewController(t))
				runner.EXPECT().Run(gomock.Any(), "sleep 30", gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ string, _ ProgressHandler) (ProcessResult, error) {
						<-ctx.Done()
						return ProcessResult{}, context.Cause(ctx)
					},
				)
				start := time.Now()

				// Act by executing the command until its usecase-owned timeout fires.
				_, err := New(runner).Execute(t.Context(), extensioncontroller.BashCommand{
					Text: "sleep 30", Timeout: mo.Some(testCase.seconds),
				}, func(extensioncontroller.BashProgress) error { return nil })

				// Assert exact timing, typed cause, and unchanged wrapping.
				require.Equal(t, testCase.duration, time.Since(start))
				var timeoutErr bashTimeoutError
				require.ErrorAs(t, err, &timeoutErr)
				require.InDelta(t, testCase.seconds, timeoutErr.seconds, 0)
				require.EqualError(t, err, "run bash command: "+timeoutErr.Error())
				require.NotErrorIs(t, err, context.Canceled)
			})
		})
	}
}

// TestExecutionContextCleanup stops the timer and preserves ordinary cleanup cancellation.
func TestExecutionContextCleanup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a timeout that must not fire after completed work.
		ctx, stop := executionContext(t.Context(), mo.Some(0.01))

		// Act by completing the command before its timer and advancing past its deadline.
		stop()
		time.Sleep(time.Second)

		// Assert cleanup cannot replace cancellation with a later timeout cause.
		require.ErrorIs(t, context.Cause(ctx), context.Canceled)
	})
}

// TestServiceExecuteParentCancellation retains the caller cause rather than a timeout outcome.
func TestServiceExecuteParentCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a caller canceled by a distinct source while its command is running.
		parent, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		cause := errors.New("caller ended operation")
		runner := NewMockProcessRunner(gomock.NewController(t))
		runner.EXPECT().Run(gomock.Any(), "sleep 30", gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ string, _ ProgressHandler) (ProcessResult, error) {
				cancel(cause)
				<-ctx.Done()
				return ProcessResult{}, context.Cause(ctx)
			},
		)

		// Act with a later timeout that must not override the parent cancellation.
		_, err := New(runner).Execute(parent, extensioncontroller.BashCommand{
			Text: "sleep 30", Timeout: mo.Some(30.0),
		}, func(extensioncontroller.BashProgress) error { return nil })

		// Assert the complete source cause remains wrapped by execution context.
		require.ErrorIs(t, err, cause)
		require.EqualError(t, err, "run bash command: caller ended operation")
	})
}
