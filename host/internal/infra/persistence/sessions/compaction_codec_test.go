//go:build !integration

package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestCompactionEntryCodecRoundTripPreservesCompletePayload verifies durable restart data for compaction markers.
func TestCompactionEntryCodecRoundTripPreservesCompletePayload(t *testing.T) {
	t.Parallel()

	// Arrange one model-backed compaction entry with usage, cost, and opaque extension details.
	entry := session.Entry{
		ID: "compaction", ParentID: mo.Some("parent"), CreatedAt: time.Unix(10, 5).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
					Selection: model.Selection{
						Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh,
					},
					Usage: mo.Some(session.TokenUsage{
						InputTokens: 3, OutputTokens: 2, CacheReadTokens: 1,
						CacheWriteTokens: 1, ReasoningTokens: 1, TotalTokens: 7,
					}),
				}),
			},
			EstimatedCost: mo.Some(session.EstimatedCost{
				Input: 0.03, Output: 0.02, CacheRead: 0.01, CacheWrite: 0.01, Total: 0.07,
			}),
			Details: mo.Some([]byte{1, 2, 3}),
		}),
	}

	// Act by encoding and decoding through the durable JSONL codec.
	encoded, err := encodeEntry(entry)
	require.NoError(t, err)
	decoded, err := decodeEntry(encoded)

	// Assert every compaction field survives restart exactly.
	require.NoError(t, err)
	require.Equal(t, entry, decoded)
}
