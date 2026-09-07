package bash

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/mo"
)

// bashTimeoutError distinguishes a tool timeout from caller cancellation.
type bashTimeoutError struct {
	// seconds contains the configured execution limit.
	seconds float64
}

// Error returns the model-visible timeout outcome.
func (e bashTimeoutError) Error() string {
	return fmt.Sprintf("bash command timed out after %g seconds", e.seconds)
}

// executionContext starts the validated timeout and returns its cancellation cleanup.
func executionContext(
	parent context.Context,
	timeout mo.Option[float64],
) (executionCtx context.Context, cleanup func()) {
	seconds, ok := timeout.Get()
	if !ok {
		return parent, func() {}
	}
	// duration retains fractional seconds and gives every positive value a timer tick.
	duration := time.Duration(seconds * float64(time.Second))
	if duration == 0 {
		duration = time.Nanosecond
	}
	ctx, cancel := context.WithCancelCause(parent)
	timer := time.AfterFunc(duration, func() {
		cancel(bashTimeoutError{seconds: seconds})
	})
	stop := func() {
		timer.Stop()
		cancel(nil)
	}
	return ctx, stop
}
