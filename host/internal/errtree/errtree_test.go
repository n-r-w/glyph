//go:build !integration

package errtree

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAllLeavesMatch verifies recursive matching for direct, wrapped, joined, mixed, and absent errors.
func TestAllLeavesMatch(t *testing.T) {
	t.Parallel()

	acceptedErr := errors.New("accepted")
	rejectedErr := errors.New("rejected")
	tests := map[string]struct {
		err      error
		expected bool
	}{
		"nil":         {err: nil, expected: false},
		"direct":      {err: acceptedErr, expected: true},
		"wrapped":     {err: fmt.Errorf("wrap: %w", acceptedErr), expected: true},
		"joined pure": {err: errors.Join(acceptedErr, fmt.Errorf("wrap: %w", acceptedErr)), expected: true},
		"joined mixed": {
			err:      errors.Join(acceptedErr, rejectedErr),
			expected: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange the table error tree and a predicate for the accepted leaf identity.
			match := func(err error) bool { return errors.Is(err, acceptedErr) }

			// Act by traversing all error leaves.
			actual := AllLeavesMatch(test.err, match)

			// Assert every leaf must satisfy the supplied policy.
			assert.Equal(t, test.expected, actual)
		})
	}
}
