//go:build !integration

package operation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFailedValidation checks diagnosed invalid outcomes and subsequent valid construction.
func TestFailedValidation(t *testing.T) {
	t.Parallel()
	for _, invalid := range []struct {
		name      string
		code      string
		hasCause  bool
		diagnoses []string
	}{
		{name: "empty code", code: "", hasCause: true, diagnoses: []string{"code is required"}},
		{name: "nil error", code: "CUSTOM", hasCause: false, diagnoses: []string{"error is required"}},
		{name: "both absent", code: "", hasCause: false, diagnoses: []string{"code is required", "error is required"}},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			t.Parallel()
			// Arrange independent causes with multiline context.
			first := errors.New("original cause Ω\nsecond line")
			second := errors.New("independent cause")
			var cause error
			if invalid.hasCause {
				cause = fmt.Errorf("context: %w", errors.Join(first, second))
			}
			// Act through the shared failed-outcome constructor without recovery.
			outcome := Failed[string](invalid.code, cause)
			// Assert the invalid construction reports INTERNAL with every diagnosis and cause.
			require.Equal(t, TerminalStateFailed, outcome.State())
			require.Equal(t, "INTERNAL", outcome.Code())
			_, present := outcome.Result()
			require.False(t, present)
			for _, err := range []error{outcome.Err(), outcome.SourceError()} {
				for _, diagnosis := range invalid.diagnoses {
					require.ErrorContains(t, err, diagnosis)
				}
				if cause != nil {
					require.ErrorContains(t, err, cause.Error())
					require.ErrorIs(t, err, cause)
					require.ErrorIs(t, err, first)
					require.ErrorIs(t, err, second)
				}
			}
			// Act again to demonstrate caller continuity and unchanged valid behavior.
			valid := Failed[string]("CUSTOM", first)
			// Assert valid construction retains its original code and error objects.
			require.Equal(t, TerminalStateFailed, valid.State())
			require.Equal(t, "CUSTOM", valid.Code())
			require.Same(t, first, valid.Err())
			require.Same(t, first, valid.SourceError())
		})
	}
}
