package tui

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestFormatSessionInformationShowsAvailableAccounting verifies /session presents token and cost statistics.
func TestFormatSessionInformationShowsAvailableAccounting(t *testing.T) {
	t.Parallel()

	// Arrange nondefault session information with available tokens, aggregate cost, and ordered groups.
	info := testStatisticsSessionInfo()
	statistics := presentationdomain.SessionStatistics{
		UserMessages: 1, ModelResponses: 2, ToolCalls: 3, ToolResults: 4, TotalMessages: 7,
		TokenUsage: mo.Some(presentationdomain.TokenUsage{
			InputTokens: 5, OutputTokens: 6, CacheReadTokens: 7,
			CacheWriteTokens: 8, ReasoningTokens: 2, TotalTokens: 26,
		}),
		EstimatedCost: mo.Some(presentationdomain.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		}),
		CostBreakdown: []presentationdomain.ProviderModelCost{
			{ProviderID: "provider-a", ModelID: "model-a", EstimatedCost: mo.Some(presentationdomain.EstimatedCost{})},
			{ProviderID: "provider-b", ModelID: "model-b", EstimatedCost: mo.None[presentationdomain.EstimatedCost]()},
		},
	}

	// Act by formatting the standard TUI /session result.
	text := formatSessionInformation(info, mo.Some(statistics))

	// Assert lifecycle, counts, tokens, aggregate cost, and ordered group availability are present.
	assert.Contains(t, text, "Session ID: active")
	assert.Contains(t, text, "Name: selected")
	assert.Contains(t, text, "Messages: 1 user, 2 model, 4 tool results, 7 total")
	assert.Contains(t, text, "Tool calls: 3")
	assert.Contains(t, text, "Tokens: 5 input, 6 output, 7 cache read, 8 cache write, 26 total")
	assert.Contains(t, text, "Reasoning tokens: 2, included in output")
	assert.Contains(t, text, "Estimated cost: $10")
	assert.Contains(t, text, "provider-a/model-a: $0")
	assert.Contains(t, text, "provider-b/model-b: unavailable")
}

// TestFormatSessionInformationShowsUnavailableAccountingWithCounts verifies absence does not hide counts.
func TestFormatSessionInformationShowsUnavailableAccountingWithCounts(t *testing.T) {
	t.Parallel()

	// Arrange available counts with unavailable aggregate token and cost totals.
	statistics := presentationdomain.SessionStatistics{
		UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
		TokenUsage:    mo.None[presentationdomain.TokenUsage](),
		EstimatedCost: mo.None[presentationdomain.EstimatedCost](), CostBreakdown: nil,
	}

	// Act by formatting the standard TUI /session result.
	text := formatSessionInformation(testStatisticsSessionInfo(), mo.Some(statistics))

	// Assert counts remain visible and both unavailable aggregates are explicit.
	assert.Contains(t, text, "Messages: 1 user, 1 model, 0 tool results, 2 total")
	assert.Contains(t, text, "Tokens: unavailable")
	assert.Contains(t, text, "Estimated cost: unavailable")
}

func testStatisticsSessionInfo() presentationdomain.SessionInfo {
	return presentationdomain.SessionInfo{
		ID: "active", Name: "selected", NamePresent: true, WorkingDirectory: "/project",
		StoragePath: "/session.jsonl", StoragePresent: true,
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
}
