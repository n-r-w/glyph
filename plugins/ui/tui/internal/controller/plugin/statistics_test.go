package plugin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestMapRequestReconstructsAvailableSessionStatistics verifies the helper-process boundary preserves token presence.
func TestMapRequestReconstructsAvailableSessionStatistics(t *testing.T) {
	t.Parallel()

	// Arrange lifecycle values, available tokens, distinct costs, and an unavailable group.
	info := uiv1.SessionInfo_builder{
		Id: new("active"), Name: new("name"), WorkingDirectory: new("/project"), StoragePath: new("/session.jsonl"),
		CreatedTime: timestamppb.New(time.Unix(1, 0)), UpdateTime: timestamppb.New(time.Unix(2, 0)),
	}.Build()
	tokens := uiv1.TokenUsage_builder{
		InputTokens: new(int64(1)), OutputTokens: new(int64(2)), CacheReadTokens: new(int64(3)),
		CacheWriteTokens: new(int64(4)), ReasoningTokens: new(int64(2)), TotalTokens: new(int64(10)),
	}.Build()
	estimatedCost := uiv1.EstimatedCost_builder{
		Input: new(1.0), Output: new(2.0), CacheRead: new(3.0), CacheWrite: new(4.0), Total: new(10.0),
	}.Build()
	costBreakdown := []*uiv1.ProviderModelCost{
		uiv1.ProviderModelCost_builder{
			ProviderId: new("provider-a"), ModelId: new("model-a"),
			EstimatedCost: uiv1.EstimatedCost_builder{
				Input: new(5.0), Output: new(6.0), CacheRead: new(7.0), CacheWrite: new(8.0), Total: new(26.0),
			}.Build(),
		}.Build(),
		uiv1.ProviderModelCost_builder{
			ProviderId: new("provider-b"), ModelId: new("model-b"), EstimatedCost: nil,
		}.Build(),
	}
	statistics := uiv1.SessionStatistics_builder{
		UserMessages: new(int64(1)), ModelResponses: new(int64(2)), ToolCalls: new(int64(3)),
		ToolResults: new(int64(4)), TotalMessages: new(int64(7)), Tokens: tokens,
		EstimatedCost: estimatedCost, CostBreakdown: costBreakdown,
	}.Build()
	//nolint:exhaustruct // The protobuf builder intentionally sets only the active oneof field.
	request := uiv1.OpenRequest_builder{
		SessionInformation: uiv1.SessionInformation_builder{Info: info, Statistics: statistics}.Build(),
	}.Build()

	// Act by reconstructing the TUI presentation event.
	event, err := mapRequest(request)

	// Assert lifecycle, tokens, all cost fields, group order, and absence match the wire values.
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventSessionInformation, event.Kind)
	assert.Equal(t, "active", event.SessionInfo.OrEmpty().ID)
	mappedStatistics, present := event.SessionStatistics.Get()
	assert.True(t, present)
	assert.Equal(t, 7, mappedStatistics.TotalMessages)
	assert.Equal(t, int64(10), mappedStatistics.TokenUsage.OrEmpty().TotalTokens)
	mappedAggregate := mappedStatistics.EstimatedCost.OrEmpty()
	assert.InDelta(t, 1, mappedAggregate.Input, 1e-12)
	assert.InDelta(t, 2, mappedAggregate.Output, 1e-12)
	assert.InDelta(t, 3, mappedAggregate.CacheRead, 1e-12)
	assert.InDelta(t, 4, mappedAggregate.CacheWrite, 1e-12)
	assert.InDelta(t, 10, mappedAggregate.Total, 1e-12)
	require.Len(t, mappedStatistics.CostBreakdown, 2)
	assert.Equal(t, "provider-a", mappedStatistics.CostBreakdown[0].ProviderID)
	assert.True(t, mappedStatistics.CostBreakdown[0].EstimatedCost.IsSome())
	mappedGroup := mappedStatistics.CostBreakdown[0].EstimatedCost.OrEmpty()
	assert.InDelta(t, 5, mappedGroup.Input, 1e-12)
	assert.InDelta(t, 6, mappedGroup.Output, 1e-12)
	assert.InDelta(t, 7, mappedGroup.CacheRead, 1e-12)
	assert.InDelta(t, 8, mappedGroup.CacheWrite, 1e-12)
	assert.InDelta(t, 26, mappedGroup.Total, 1e-12)
	assert.True(t, mappedStatistics.CostBreakdown[1].EstimatedCost.IsAbsent())
}
