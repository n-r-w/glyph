//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapToolResultPreservesErrorCategory verifies that the public tool result carries the category of every member
// of the closed set and none for a result without a category.
func TestMapToolResultPreservesErrorCategory(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		category mo.Option[agent.ToolErrorCategory]
		want     mo.Option[programmaticv1.ToolErrorCategory]
	}{
		"none": {category: mo.None[agent.ToolErrorCategory](), want: mo.None[programmaticv1.ToolErrorCategory]()},
		"unavailable tool": {
			category: mo.Some(agent.ToolErrorUnavailableTool),
			want:     mo.Some(programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL),
		},
		"invalid arguments": {
			category: mo.Some(agent.ToolErrorInvalidArguments),
			want:     mo.Some(programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS),
		},
		"tool failure": {
			category: mo.Some(agent.ToolErrorFailure),
			want:     mo.Some(programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE),
		},
		"policy rejection": {
			category: mo.Some(agent.ToolErrorPolicyRejection),
			want:     mo.Some(programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a domain error result with the scenario category.
			result := agent.ToolResult{
				CallID: "call", ToolName: "read", Contents: tool.TextContents("text"), IsError: true,
				ErrorCategory: test.category,
			}

			// Act through the public projection and the protobuf mapping.
			mapped, err := mapToolResult(MapToolResult(result))

			// Assert the presence and value of the wire category.
			require.NoError(t, err)
			assert.True(t, mapped.GetIsError())
			category, present := test.want.Get()
			assert.Equal(t, present, mapped.HasErrorCategory())
			assert.Equal(t, category, mapped.GetErrorCategory())
		})
	}
}

// TestMapToolResultRejectsUnknownErrorCategory verifies that a category outside the closed set is not published.
func TestMapToolResultRejectsUnknownErrorCategory(t *testing.T) {
	t.Parallel()

	// Arrange a public result with a category outside the closed set.
	result := MapToolResult(agent.ToolResult{
		CallID: "call", ToolName: "read", Contents: tool.TextContents("text"), IsError: true,
		ErrorCategory: mo.Some(agent.ToolErrorCategory(99)),
	})

	// Act through the protobuf mapping.
	_, err := mapToolResult(result)

	// Assert the explicit failure.
	require.EqualError(t, err, "map tool error category: unknown value 99")
}
