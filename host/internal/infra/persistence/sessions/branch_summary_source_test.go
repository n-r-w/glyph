//go:build !integration

package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestBranchSummarySourceRoundTrip verifies storage retains extension and model producers with nested usage.
func TestBranchSummarySourceRoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange independent extension, unpriced model, and reported-zero-usage model sources.
	sources := []session.BranchSummarySource{
		{ExtensionID: mo.Some("producer"), Model: mo.None[session.BranchSummaryModelSource]()},
		{ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
			Selection: model.Selection{
				Provider:        "external",
				Model:           "unconfigured",
				ReasoningChoice: model.ReasoningChoiceHigh,
			},
			Usage: mo.None[session.TokenUsage](),
		})},
		{ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
			Selection: model.Selection{
				Provider:        "external",
				Model:           "unconfigured",
				ReasoningChoice: model.ReasoningChoiceOff,
			},
			Usage: mo.Some(session.TokenUsage{}),
		})},
	}
	for _, source := range sources {
		entry := session.Entry{
			ID:            "summary",
			ParentID:      mo.None[string](),
			CreatedAt:     time.Unix(1, 0).UTC(),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.Some(session.BranchSummaryEntry{
				Summary:       "context",
				FirstEntryID:  "first",
				LastEntryID:   "last",
				Source:        source,
				EstimatedCost: mo.None[session.EstimatedCost](),
			}),
		}

		// Act through the persisted entry codec without a model catalogue.
		encoded, err := encodeEntry(entry)
		require.NoError(t, err)
		decoded, err := decodeEntry(encoded)

		// Assert source identity and usage presence survive without invented cost.
		require.NoError(t, err)
		assert.Equal(t, entry, decoded)
	}
}
