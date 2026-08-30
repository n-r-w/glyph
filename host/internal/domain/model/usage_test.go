package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeUsageClampsEveryNegativeProviderBucket verifies malformed provider counters cannot escape normalization.
func TestNormalizeUsageClampsEveryNegativeProviderBucket(t *testing.T) {
	t.Parallel()

	// Arrange negative provider buckets and an unrelated provider total.
	usage := Usage{
		InputTokens: -1, OutputTokens: -2, CachedInputTokens: -3,
		CacheWriteTokens: -4, ReasoningTokens: -5, TotalTokens: 99,
	}

	// Act by normalizing provider accounting.
	normalized := usage.Normalize()

	// Assert every bucket and the derived total are nonnegative zero.
	assert.Equal(t, Usage{}, normalized)
}
