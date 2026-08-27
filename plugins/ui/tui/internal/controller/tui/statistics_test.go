package tui

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestFormatSessionInformationShowsAvailableTokenBuckets verifies /session presents complete statistics without losing info.
func TestFormatSessionInformationShowsAvailableTokenBuckets(t *testing.T) {
	t.Parallel()

	// Arrange nondefault session information and available normalized token totals.
	info := testStatisticsSessionInfo()
	statistics := presentationdomain.SessionStatistics{
		UserMessages: 1, ModelResponses: 2, ToolCalls: 3, ToolResults: 4, TotalMessages: 7,
		TokenUsage: mo.Some(presentationdomain.TokenUsage{
			InputTokens: 5, OutputTokens: 6, CacheReadTokens: 7,
			CacheWriteTokens: 8, ReasoningTokens: 2, TotalTokens: 26,
		}),
	}

	// Act by formatting the standard TUI /session result.
	text := formatSessionInformation(info, mo.Some(statistics))

	// Assert existing fields, counts, disjoint buckets, and reasoning subset text are present.
	assert.Contains(t, text, "Session ID: active")
	assert.Contains(t, text, "Name: selected")
	assert.Contains(t, text, "Messages: 1 user, 2 model, 4 tool results, 7 total")
	assert.Contains(t, text, "Tool calls: 3")
	assert.Contains(t, text, "Tokens: 5 input, 6 output, 7 cache read, 8 cache write, 26 total")
	assert.Contains(t, text, "Reasoning tokens: 2, included in output")
}

// TestFormatSessionInformationShowsUnavailableTokensWithCounts verifies token absence does not hide counts.
func TestFormatSessionInformationShowsUnavailableTokensWithCounts(t *testing.T) {
	t.Parallel()

	// Arrange available counts and unavailable aggregate token totals.
	statistics := presentationdomain.SessionStatistics{
		UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
		TokenUsage: mo.None[presentationdomain.TokenUsage](),
	}

	// Act by formatting the standard TUI /session result.
	text := formatSessionInformation(testStatisticsSessionInfo(), mo.Some(statistics))

	// Assert counts remain visible and token unavailability is explicit.
	assert.Contains(t, text, "Messages: 1 user, 1 model, 0 tool results, 2 total")
	assert.Contains(t, text, "Tokens: unavailable")
}

func testStatisticsSessionInfo() presentationdomain.SessionInfo {
	return presentationdomain.SessionInfo{
		ID: "active", Name: "selected", NamePresent: true, WorkingDirectory: "/project",
		StoragePath: "/session.jsonl", StoragePresent: true,
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
}
