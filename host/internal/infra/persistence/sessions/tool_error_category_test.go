//go:build !integration

package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// categorizedToolEntry returns a persisted tool-result entry with the given error flag and category.
func categorizedToolEntry(isError bool, category mo.Option[agent.ToolErrorCategory]) session.Entry {
	return session.Entry{
		ParentID: mo.None[string](), ID: "tool", CreatedAt: time.Date(2026, 8, 27, 3, 0, 0, 0, time.UTC),
		Information: mo.None[session.Information](), User: mo.None[model.Message](),
		Model: mo.None[session.ModelResponse](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "read", Contents: tool.TextContents("text"), IsError: isError,
			ErrorCategory: category,
		}),
		Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
		BranchSummary: mo.None[session.BranchSummaryEntry](), ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
}

// TestToolErrorCategoryRoundTrip verifies that every category and the absence of a category survive JSONL.
func TestToolErrorCategoryRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		isError  bool
		category mo.Option[agent.ToolErrorCategory]
	}{
		"success without category": {isError: false, category: mo.None[agent.ToolErrorCategory]()},
		"error without category":   {isError: true, category: mo.None[agent.ToolErrorCategory]()},
		"unavailable tool":         {isError: true, category: mo.Some(agent.ToolErrorUnavailableTool)},
		"invalid arguments":        {isError: true, category: mo.Some(agent.ToolErrorInvalidArguments)},
		"tool failure":             {isError: true, category: mo.Some(agent.ToolErrorFailure)},
		"policy rejection":         {isError: true, category: mo.Some(agent.ToolErrorPolicyRejection)},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange the entry with the scenario category.
			entry := categorizedToolEntry(test.isError, test.category)

			// Act by encoding and decoding the entry.
			encoded, err := encodeEntry(entry)
			require.NoError(t, err)
			decoded, err := decodeEntry(encoded)

			// Assert the category and the error flag.
			require.NoError(t, err)
			result := decoded.ToolResult.MustGet()
			assert.Equal(t, test.category, result.ErrorCategory)
			assert.Equal(t, test.isError, result.IsError)
		})
	}
}

// TestToolErrorCategoryDecodeRejectsInvalidValue verifies that a stored category outside the closed set or on a
// successful result is not accepted.
func TestToolErrorCategoryDecodeRejectsInvalidValue(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		entry     session.Entry
		errorText string
	}{
		"outside the closed set": {
			entry:     categorizedToolEntry(true, mo.Some(agent.ToolErrorCategory(99))),
			errorText: "invalid tool result entry: tool error category 99 is outside the closed set",
		},
		"on a successful result": {
			entry:     categorizedToolEntry(false, mo.Some(agent.ToolErrorFailure)),
			errorText: "invalid tool result entry: tool error category requires an error result",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange the stored line of the invalid entry.
			encoded, err := encodeEntry(test.entry)
			require.NoError(t, err)

			// Act by decoding the line.
			_, err = decodeEntry(encoded)

			// Assert the complete rejection text.
			require.ErrorContains(t, err, test.errorText)
		})
	}
}
