package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapOpenRequestAcceptsSessionStatisticsQuery verifies the public command reaches the Host model.
func TestMapOpenRequestAcceptsSessionStatisticsQuery(t *testing.T) {
	t.Parallel()

	// Arrange a correlated GetSessionStats request.
	//nolint:exhaustruct // The protobuf builder intentionally sets only the active oneof field.
	request := programmaticv1.OpenRequest_builder{
		CorrelationId: new("stats"), GetSessionStats: programmaticv1.GetSessionStats_builder{}.Build(),
	}.Build()

	// Act by mapping the public request.
	command, err := mapOpenRequest(request)

	// Assert the statistics command and correlation ID are preserved.
	require.NoError(t, err)
	assert.Equal(t, CommandGetSessionStats, command.Kind)
	assert.Equal(t, "stats", command.CorrelationID)
}

// TestMapResponsePreservesSessionStatisticsAvailability verifies counts and optional tokens map independently.
func TestMapResponsePreservesSessionStatisticsAvailability(t *testing.T) {
	t.Parallel()

	// Arrange complete counts with unavailable tokens.
	statistics := session.Statistics{
		UserMessages: 2, ModelResponses: 3, ToolCalls: 4, ToolResults: 5, TotalMessages: 10,
		TokenUsage: mo.None[session.TokenUsage](), EstimatedCost: mo.None[session.EstimatedCost](), CostBreakdown: nil,
	}
	response := Response{
		CorrelationID: "stats", Kind: ResponseSessionStats,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.Some(statistics), Rejection: mo.None[Rejection](),
	}

	// Act by mapping the Host response to protobuf.
	wire, err := mapResponse(response)

	// Assert counts remain present and the token message remains absent.
	require.NoError(t, err)
	mapped := wire.GetCommandResponse().GetSessionStats().GetStatistics()
	assert.Equal(t, int64(2), mapped.GetUserMessages())
	assert.Equal(t, int64(3), mapped.GetModelResponses())
	assert.Equal(t, int64(4), mapped.GetToolCalls())
	assert.Equal(t, int64(5), mapped.GetToolResults())
	assert.Equal(t, int64(10), mapped.GetTotalMessages())
	assert.False(t, mapped.HasTokens())
}

// TestMapResponsePreservesEstimatedCostAndOrderedBreakdown verifies public cost presence and values.
func TestMapResponsePreservesEstimatedCostAndOrderedBreakdown(t *testing.T) {
	t.Parallel()

	// Arrange available aggregate cost with available-zero and unavailable provider-model groups.
	aggregate := session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}
	statistics := session.Statistics{
		UserMessages: 0, ModelResponses: 2, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
		TokenUsage: mo.None[session.TokenUsage](), EstimatedCost: mo.Some(aggregate),
		CostBreakdown: []session.ProviderModelCost{
			{Provider: "provider-a", Model: "model-a", EstimatedCost: mo.Some(session.EstimatedCost{
				Input: 5, Output: 6, CacheRead: 7, CacheWrite: 8, Total: 26,
			})},
			{Provider: "provider-b", Model: "model-b", EstimatedCost: mo.None[session.EstimatedCost]()},
		},
	}
	response := Response{
		CorrelationID: "stats", Kind: ResponseSessionStats,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.Some(statistics), Rejection: mo.None[Rejection](),
	}

	// Act by mapping the Host statistics response to protobuf.
	wire, err := mapResponse(response)

	// Assert aggregate and group buckets, unavailable presence, and order are exact.
	require.NoError(t, err)
	mapped := wire.GetCommandResponse().GetSessionStats().GetStatistics()
	require.True(t, mapped.HasEstimatedCost())
	mappedAggregate := mapped.GetEstimatedCost()
	assert.InDelta(t, 1, mappedAggregate.GetInput(), 1e-12)
	assert.InDelta(t, 2, mappedAggregate.GetOutput(), 1e-12)
	assert.InDelta(t, 3, mappedAggregate.GetCacheRead(), 1e-12)
	assert.InDelta(t, 4, mappedAggregate.GetCacheWrite(), 1e-12)
	assert.InDelta(t, 10, mappedAggregate.GetTotal(), 1e-12)
	require.Len(t, mapped.GetCostBreakdown(), 2)
	assert.Equal(t, "provider-a", mapped.GetCostBreakdown()[0].GetProviderId())
	assert.True(t, mapped.GetCostBreakdown()[0].HasEstimatedCost())
	mappedGroup := mapped.GetCostBreakdown()[0].GetEstimatedCost()
	assert.InDelta(t, 5, mappedGroup.GetInput(), 1e-12)
	assert.InDelta(t, 6, mappedGroup.GetOutput(), 1e-12)
	assert.InDelta(t, 7, mappedGroup.GetCacheRead(), 1e-12)
	assert.InDelta(t, 8, mappedGroup.GetCacheWrite(), 1e-12)
	assert.InDelta(t, 26, mappedGroup.GetTotal(), 1e-12)
	assert.Equal(t, "provider-b", mapped.GetCostBreakdown()[1].GetProviderId())
	assert.False(t, mapped.GetCostBreakdown()[1].HasEstimatedCost())
}
