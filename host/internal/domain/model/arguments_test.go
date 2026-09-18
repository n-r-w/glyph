//go:build !integration

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewToolCallArgumentsPreservesValidatedJSON verifies finalized arguments retain detached accepted bytes.
func TestNewToolCallArgumentsPreservesValidatedJSON(t *testing.T) {
	t.Parallel()

	// Arrange an object whose whitespace, escape spelling, and number spelling are significant.
	input := []byte("{ \"text\": \"\\u0061\", \"number\": 1.00 }")
	expected := string(input)

	// Act by validating, retaining, and obtaining a byte projection of the argument value.
	arguments, err := NewToolCallArguments(input)
	require.NoError(t, err)
	input[0] = '['
	projected := arguments.Bytes()
	projected[0] = '['

	// Assert neither caller-owned input nor projected bytes can mutate the retained value.
	assert.Equal(t, expected, arguments.String())
	assert.Equal(t, len(expected), arguments.Len())
}

// TestNewToolCallArgumentsPreservesNull verifies the prior map decoder's accepted null shape remains valid.
func TestNewToolCallArgumentsPreservesNull(t *testing.T) {
	t.Parallel()

	// Arrange the top-level null value accepted by JSON decoding into a map.
	input := []byte(`null`)

	// Act by validating and retaining the argument value.
	arguments, err := NewToolCallArguments(input)

	// Assert the exact accepted value remains available.
	require.NoError(t, err)
	assert.Equal(t, "null", arguments.String())
}

// TestNewToolCallArgumentsRejectsNonObjectJSON verifies arrays and non-null scalars keep the prior map shape.
func TestNewToolCallArgumentsRejectsNonObjectJSON(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][]byte{
		"array":   []byte(`[]`),
		"string":  []byte(`"value"`),
		"number":  []byte(`1`),
		"boolean": []byte(`true`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one valid JSON value outside the established object-or-null shape.

			// Act by validating the finalized argument value.
			_, err := NewToolCallArguments(input)

			// Assert the JSON decoder's complete shape error remains available.
			require.Error(t, err)
			assert.ErrorContains(t, err, "unmarshal JSON")
			assert.ErrorContains(t, err, "into Go map[string]interface {}")
		})
	}
}

// TestNewToolCallArgumentsRejectsInvalidJSON verifies malformed finalized arguments fail at construction.
func TestNewToolCallArgumentsRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	// Arrange malformed finalized JSON.
	input := []byte(`{"path":`)

	// Act by validating the argument value.
	_, err := NewToolCallArguments(input)

	// Assert malformed JSON is rejected with its parser cause.
	require.Error(t, err)
	assert.ErrorContains(t, err, "jsontext:")
}
