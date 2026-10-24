//go:build !integration

package extensionv1

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestClassifiedErrorValidation checks invalid arguments and subsequent valid construction.
func TestClassifiedErrorValidation(t *testing.T) {
	t.Parallel()
	for _, constructor := range []struct {
		name      string
		makeError func(string, error) error
		rejection bool
	}{
		{name: "Reject", makeError: Reject, rejection: true},
		{name: "Fail", makeError: Fail, rejection: false},
	} {
		for _, invalid := range []struct {
			name      string
			code      string
			hasCause  bool
			diagnosis string
		}{
			{name: "empty code", code: "", hasCause: true, diagnosis: "code is required"},
			{name: "nil cause", code: "CUSTOM", hasCause: false, diagnosis: "cause is required"},
			{name: "both absent", code: "", hasCause: false, diagnosis: "code is required"},
		} {
			t.Run(constructor.name+"/"+invalid.name, func(t *testing.T) {
				t.Parallel()
				// Arrange a multi-branch cause with complete multiline text.
				first := errors.New("first cause Ω\nsecond line")
				second := errors.New("independent cause")
				var cause error
				if invalid.hasCause {
					cause = fmt.Errorf("context: %w", errors.Join(first, second))
				}
				// Act through the public constructor without a recovery wrapper.
				err := constructor.makeError(invalid.code, cause)
				// Assert validation reports the invalid argument and retains supplied causes without adding a classified wrapper.
				require.ErrorContains(t, err, invalid.diagnosis)
				_, rejected := errors.AsType[*RejectionError](err)
				_, failed := errors.AsType[*FailureError](err)
				require.False(t, rejected)
				require.False(t, failed)
				if cause != nil {
					require.ErrorContains(t, err, cause.Error())
					require.ErrorIs(t, err, cause)
					require.ErrorIs(t, err, first)
					require.ErrorIs(t, err, second)
				}
				// Act again with valid arguments.
				valid := constructor.makeError("CUSTOM", first)
				// Assert valid construction retains the code and original cause.
				require.Equal(t, first.Error(), valid.Error())
				require.Same(t, first, errors.Unwrap(valid))
				if constructor.rejection {
					classified, ok := errors.AsType[*RejectionError](valid)
					require.True(t, ok)
					require.Equal(t, "CUSTOM", classified.Code())
				} else {
					classified, ok := errors.AsType[*FailureError](valid)
					require.True(t, ok)
					require.Equal(t, "CUSTOM", classified.Code())
				}
			})
		}
	}
}
