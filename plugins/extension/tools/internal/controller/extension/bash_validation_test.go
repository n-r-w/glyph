//go:build !integration

package extension

import (
	"math"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestValidateBashTimeout preserves optional, fractional, and maximum duration validation.
func TestValidateBashTimeout(t *testing.T) {
	t.Parallel()

	// Arrange public timeout values at each validation boundary.
	maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
	for _, testCase := range []struct {
		// name identifies the input boundary.
		name string
		// timeout contains the optional input seconds.
		timeout mo.Option[float64]
		// message is empty for accepted values and exact for validation errors.
		message string
	}{
		{name: "absent", timeout: mo.None[float64](), message: ""},
		{name: "fractional", timeout: mo.Some(0.01), message: ""},
		{name: "sub-nanosecond", timeout: mo.Some(1e-10), message: ""},
		{name: "maximum", timeout: mo.Some(maximumSeconds), message: ""},
		{
			name: "above maximum", timeout: mo.Some(math.Nextafter(maximumSeconds, math.Inf(1))),
			message: "bash timeout exceeds supported duration",
		},
		{name: "zero", timeout: mo.Some(0.0), message: "bash timeout must be positive"},
		{name: "negative", timeout: mo.Some(-1.0), message: "bash timeout must be positive"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Act by validating intent without creating an execution context.
			err := validateBashTimeout(testCase.timeout)

			// Assert the accepted range and exact validation cause remain unchanged.
			if testCase.message == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, testCase.message)
			}
		})
	}
}
