package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestMapFramePreservesSessionInformationAndStatistics verifies Host UI protobuf mapping keeps both payloads.
func TestMapFramePreservesSessionInformationAndStatistics(t *testing.T) {
	t.Parallel()

	// Arrange lifecycle fields, present-zero token usage, and distinct aggregate and group costs.
	info := session.Info{
		ID: "active", Name: mo.Some("name"), WorkingDirectory: "/project", StoragePath: mo.Some("/session.jsonl"),
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
	statistics := session.Statistics{
		UserMessages: 1, ModelResponses: 2, ToolCalls: 3, ToolResults: 4, TotalMessages: 7,
		TokenUsage: mo.Some(session.TokenUsage{}),
		EstimatedCost: mo.Some(session.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		}),
		CostBreakdown: []session.ProviderModelCost{
			{Provider: "provider-a", Model: "model-a", EstimatedCost: mo.Some(session.EstimatedCost{
				Input: 5, Output: 6, CacheRead: 7, CacheWrite: 8, Total: 26,
			})},
			{Provider: "provider-b", Model: "model-b", EstimatedCost: mo.None[session.EstimatedCost]()},
		},
	}
	frame := domainui.Frame{
		Kind:                domainui.FrameSessionInformation,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.Some(info),
		Sessions:            nil,
		SessionEntries:      nil,
		SessionStatistics:   mo.Some(statistics),
		SessionTree:         mo.None[domainui.SessionTree](),
		TreeNavigation:      mo.None[domainui.TreeNavigationResult](),
		TreeFailure:         mo.None[domainui.TreeFailure](),
	}

	// Act by mapping the Host frame to the UI protobuf request.
	request, err := mapFrame(frame)

	// Assert lifecycle, token availability, all cost fields, group order, and absence survive.
	require.NoError(t, err)
	mapped := request.GetSessionInformation()
	assert.Equal(t, "active", mapped.GetInfo().GetId())
	assert.Equal(t, "name", mapped.GetInfo().GetName())
	assert.Equal(t, "/project", mapped.GetInfo().GetWorkingDirectory())
	assert.Equal(t, "/session.jsonl", mapped.GetInfo().GetStoragePath())
	assert.Equal(t, int64(7), mapped.GetStatistics().GetTotalMessages())
	assert.True(t, mapped.GetStatistics().HasTokens())
	assert.Zero(t, mapped.GetStatistics().GetTokens().GetTotalTokens())
	mappedAggregate := mapped.GetStatistics().GetEstimatedCost()
	assert.InDelta(t, 1, mappedAggregate.GetInput(), 1e-12)
	assert.InDelta(t, 2, mappedAggregate.GetOutput(), 1e-12)
	assert.InDelta(t, 3, mappedAggregate.GetCacheRead(), 1e-12)
	assert.InDelta(t, 4, mappedAggregate.GetCacheWrite(), 1e-12)
	assert.InDelta(t, 10, mappedAggregate.GetTotal(), 1e-12)
	require.Len(t, mapped.GetStatistics().GetCostBreakdown(), 2)
	assert.Equal(t, "provider-a", mapped.GetStatistics().GetCostBreakdown()[0].GetProviderId())
	assert.True(t, mapped.GetStatistics().GetCostBreakdown()[0].HasEstimatedCost())
	mappedGroup := mapped.GetStatistics().GetCostBreakdown()[0].GetEstimatedCost()
	assert.InDelta(t, 5, mappedGroup.GetInput(), 1e-12)
	assert.InDelta(t, 6, mappedGroup.GetOutput(), 1e-12)
	assert.InDelta(t, 7, mappedGroup.GetCacheRead(), 1e-12)
	assert.InDelta(t, 8, mappedGroup.GetCacheWrite(), 1e-12)
	assert.InDelta(t, 26, mappedGroup.GetTotal(), 1e-12)
	assert.False(t, mapped.GetStatistics().GetCostBreakdown()[1].HasEstimatedCost())
}
