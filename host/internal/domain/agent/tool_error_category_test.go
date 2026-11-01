//go:build !integration

package agent

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestToolErrorCategoryValid verifies that exactly the members of the closed set are valid and named.
func TestToolErrorCategoryValid(t *testing.T) {
	t.Parallel()

	// Arrange the closed set and values outside it.
	closed := []ToolErrorCategory{ToolErrorUnavailableTool, ToolErrorInvalidArguments, ToolErrorFailure}
	outside := []ToolErrorCategory{0, ToolErrorFailure + 1, 255}

	// Act and assert through validity and the model-visible name.
	names := make(map[string]struct{}, len(closed))
	for _, category := range closed {
		assert.True(t, category.Valid())
		assert.NotEmpty(t, category.Name())
		names[category.Name()] = struct{}{}
	}
	assert.Len(t, names, len(closed))
	for _, category := range outside {
		assert.False(t, category.Valid())
		assert.Empty(t, category.Name())
	}
}

// TestToolResultValidateErrorCategory verifies that only a member of the closed set on an error result is accepted.
func TestToolResultValidateErrorCategory(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		isError   bool
		category  mo.Option[ToolErrorCategory]
		errorText string
	}{
		"no category on success": {isError: false, category: mo.None[ToolErrorCategory](), errorText: ""},
		"no category on error":   {isError: true, category: mo.None[ToolErrorCategory](), errorText: ""},
		"category on error":      {isError: true, category: mo.Some(ToolErrorInvalidArguments), errorText: ""},
		"category on success": {
			isError: false, category: mo.Some(ToolErrorFailure),
			errorText: "tool error category requires an error result",
		},
		"category outside the set": {
			isError: true, category: mo.Some(ToolErrorCategory(99)),
			errorText: "tool error category 99 is outside the closed set",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a result with the scenario flags.
			result := ToolResult{
				CallID: "call", ToolName: "read", Contents: tool.TextContents("text"), IsError: test.isError,
				ErrorCategory: test.category,
			}

			// Act through the validation.
			err := result.ValidateErrorCategory()

			// Assert the accepted or rejected category.
			if test.errorText == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.errorText)
		})
	}
}
