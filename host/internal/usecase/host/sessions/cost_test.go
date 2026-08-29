package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

const costTolerance = 1e-12

// TestAppendSelectsExclusivePricingTierAndChargesDisjointBuckets verifies request-wide tier selection and cost buckets.
func TestAppendSelectsExclusivePricingTierAndChargesDisjointBuckets(t *testing.T) {
	t.Parallel()

	// Arrange base rates and two request-wide exclusive tiers.
	pricing := model.Pricing{
		Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4,
		Tiers: []model.PricingTier{
			{InputTokensAbove: 100, Input: 10, Output: 20, CacheRead: 30, CacheWrite: 40},
			{InputTokensAbove: 200, Input: 100, Output: 200, CacheRead: 300, CacheWrite: 400},
		},
	}
	testCases := map[string]struct {
		usage model.Usage
		rates model.PricingTier
	}{
		"below first threshold": {
			usage: costUsage(50, 10, 30, 19, 7),
			rates: model.PricingTier{InputTokensAbove: 0, Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
		},
		"at first threshold": {
			usage: costUsage(50, 10, 30, 20, 7),
			rates: model.PricingTier{InputTokensAbove: 0, Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
		},
		"above first threshold": {
			usage: costUsage(51, 10, 30, 20, 7),
			rates: pricing.Tiers[0],
		},
		"at second threshold": {
			usage: costUsage(100, 10, 50, 50, 7),
			rates: pricing.Tiers[0],
		},
		"above second threshold": {
			usage: costUsage(101, 10, 50, 50, 7),
			rates: pricing.Tiers[1],
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service, captured := costAppendService(t, mo.Some(testCase.usage), mo.Some(pricing))

			// Act by appending a response whose provider metadata names a different model.
			err := service.Append(t.Context(), modelHistoryEntry(mo.Some(testCase.usage)))

			// Assert the requested model selected one rate set and reasoning was not charged again.
			require.NoError(t, err)
			entry := <-captured
			actual, present := entry.EstimatedCost.Get()
			require.True(t, present)
			expected := expectedCost(testCase.usage, testCase.rates)
			assert.InDelta(t, expected.Input, actual.Input, costTolerance)
			assert.InDelta(t, expected.Output, actual.Output, costTolerance)
			assert.InDelta(t, expected.CacheRead, actual.CacheRead, costTolerance)
			assert.InDelta(t, expected.CacheWrite, actual.CacheWrite, costTolerance)
			assert.InDelta(t, expected.Total, actual.Total, costTolerance)
			assert.Equal(t, mo.Some(model.ID("response-metadata-model")), entry.Model.OrEmpty().ResponseModel)
		})
	}
}

// TestAppendPreservesKnownZeroAndUnavailableCost verifies cost presence requires both usage and exact pricing.
func TestAppendPreservesKnownZeroAndUnavailableCost(t *testing.T) {
	t.Parallel()

	// Arrange present zero usage and pricing alongside each missing input.
	zeroUsage := costUsage(0, 0, 0, 0, 0)
	zeroPricing := model.Pricing{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Tiers: nil}
	testCases := map[string]struct {
		usage   mo.Option[model.Usage]
		pricing mo.Option[model.Pricing]
		present bool
	}{
		"known zero":      {usage: mo.Some(zeroUsage), pricing: mo.Some(zeroPricing), present: true},
		"missing usage":   {usage: mo.None[model.Usage](), pricing: mo.Some(zeroPricing), present: false},
		"missing pricing": {usage: mo.Some(zeroUsage), pricing: mo.None[model.Pricing](), present: false},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service, captured := costAppendService(t, testCase.usage, testCase.pricing)

			// Act by appending the terminal model response.
			err := service.Append(t.Context(), modelHistoryEntry(testCase.usage))

			// Assert known zero remains present while either missing input keeps cost absent.
			require.NoError(t, err)
			cost, present := (<-captured).EstimatedCost.Get()
			assert.Equal(t, testCase.present, present)
			if testCase.present {
				assert.Equal(t, session.EstimatedCost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0}, cost)
			}
		})
	}
}

// TestStatisticsReturnsAvailableZeroCostForEmptySession verifies the empty-session accounting identity.
func TestStatisticsReturnsAvailableZeroCostForEmptySession(t *testing.T) {
	t.Parallel()

	// Arrange an empty durable entry sequence.
	entries := []session.Entry(nil)

	// Act by deriving session statistics.
	statistics := statisticsFromEntries(entries)

	// Assert aggregate zero is available and no provider-model group exists.
	assert.Equal(t, mo.Some(session.EstimatedCost{
		Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
	}), statistics.EstimatedCost)
	assert.Empty(t, statistics.CostBreakdown)
}

// TestStatisticsAggregatesCostByDeterministicProviderModelOrder verifies independent group completeness.
func TestStatisticsAggregatesCostByDeterministicProviderModelOrder(t *testing.T) {
	t.Parallel()

	// Arrange available and unavailable costs across provider-model groups in reverse order.
	entries := []session.Entry{
		costStatisticsEntry("provider-b", "model-z", mo.Some(session.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		})),
		costStatisticsEntry("provider-a", "shared", mo.Some(session.EstimatedCost{
			Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
		})),
		costStatisticsEntry("provider-a", "shared", mo.None[session.EstimatedCost]()),
	}

	// Act by deriving aggregate and provider-model statistics.
	statistics := statisticsFromEntries(entries)

	// Assert the aggregate and affected group are unavailable while the other group remains complete.
	assert.True(t, statistics.EstimatedCost.IsAbsent())
	require.Len(t, statistics.CostBreakdown, 2)
	assert.Equal(t, model.ProviderID("provider-a"), statistics.CostBreakdown[0].Provider)
	assert.Equal(t, model.ID("shared"), statistics.CostBreakdown[0].Model)
	assert.True(t, statistics.CostBreakdown[0].EstimatedCost.IsAbsent())
	assert.Equal(t, model.ProviderID("provider-b"), statistics.CostBreakdown[1].Provider)
	assert.Equal(t, model.ID("model-z"), statistics.CostBreakdown[1].Model)
	assert.Equal(t, mo.Some(session.EstimatedCost{
		Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
	}), statistics.CostBreakdown[1].EstimatedCost)
}

// TestStatisticsSumsEveryPersistedCostBucket verifies complete aggregate and group values.
func TestStatisticsSumsEveryPersistedCostBucket(t *testing.T) {
	t.Parallel()

	// Arrange two available costs for one configured provider-model pair.
	entries := []session.Entry{
		costStatisticsEntry("provider", "model", mo.Some(session.EstimatedCost{
			Input: 0.1, Output: 0.2, CacheRead: 0.03, CacheWrite: 0.04, Total: 0.37,
		})),
		costStatisticsEntry("provider", "model", mo.Some(session.EstimatedCost{
			Input: 0.4, Output: 0.5, CacheRead: 0.06, CacheWrite: 0.07, Total: 1.03,
		})),
	}

	// Act by deriving aggregate and grouped cost.
	statistics := statisticsFromEntries(entries)

	// Assert all bucket sums use explicit floating-point tolerance.
	aggregate := statistics.EstimatedCost.OrEmpty()
	assert.InDelta(t, 0.5, aggregate.Input, costTolerance)
	assert.InDelta(t, 0.7, aggregate.Output, costTolerance)
	assert.InDelta(t, 0.09, aggregate.CacheRead, costTolerance)
	assert.InDelta(t, 0.11, aggregate.CacheWrite, costTolerance)
	assert.InDelta(t, 1.4, aggregate.Total, costTolerance)
	require.Len(t, statistics.CostBreakdown, 1)
	group := statistics.CostBreakdown[0].EstimatedCost.OrEmpty()
	assert.InDelta(t, aggregate.Total, group.Total, costTolerance)
}

// TestResumePreservesStoredCostWithoutPricingLookup verifies settings changes cannot recalculate durable cost.
func TestResumePreservesStoredCostWithoutPricingLookup(t *testing.T) {
	t.Parallel()

	// Arrange a loaded entry with stored cost and a pricing mock that permits no lookup.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	pricing := NewMockPricingCatalog(controller)
	storedCost := session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}
	loaded := LoadedSession{
		Header: session.Header{
			Version: formatVersion, ID: "stored", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/stored.jsonl",
		Tree: mustSessionTree([]session.Entry{
			costStatisticsEntry("provider", "model", mo.Some(storedCost)),
		}),
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
	repository.EXPECT().Load(gomock.Any(), session.ID("stored")).Return(loaded, nil)
	service := New(repository, nil, nil, pricing, "/project")

	// Act by resuming the stored session under the replacement pricing catalog.
	_, err := service.ResumeActive(t.Context(), "stored")

	// Assert the stored cost is returned unchanged without consulting current pricing.
	require.NoError(t, err)
	entries := service.ActiveEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, mo.Some(storedCost), entries[0].EstimatedCost)
}

// costAppendService creates one initialized active snapshot and captures the repository entry.
func costAppendService(
	t *testing.T,
	usage mo.Option[model.Usage],
	pricing mo.Option[model.Pricing],
) (*Service, <-chan session.Entry) {
	t.Helper()
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	catalog := NewMockPricingCatalog(controller)
	if usage.IsSome() {
		catalog.EXPECT().Pricing(model.ProviderID("configured-provider"), model.ID("requested-model")).Return(pricing)
	}
	ids.EXPECT().NewID().Return("model-entry", nil)
	clock.EXPECT().Now().Return(time.Unix(10, 0).UTC())
	captured := make(chan session.Entry, 1)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ any, command ApplyCommand) (ApplyResult, error) {
			captured <- command.Mutation.Entry.MustGet()
			return ApplyResult{StoragePath: "/sessions/active.jsonl"}, nil
		},
	)
	service := New(repository, ids, clock, catalog, "/project")
	service.active = LoadedSession{
		Header: session.Header{
			Version: formatVersion, ID: "active", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/active.jsonl", Tree: session.Tree{}, Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
	return service, captured
}

// modelHistoryEntry returns one terminal response with distinct requested and response model identities.
func modelHistoryEntry(usage mo.Option[model.Usage]) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryModel,
		User: mo.None[model.Message](),
		Model: mo.Some(model.Response{
			Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.Some(model.ProviderID("configured-provider")), Model: mo.Some(model.ID("requested-model")),
			ResponseModel: mo.Some(model.ID("response-metadata-model")), ResponseID: mo.None[string](),
			Usage: usage, Diagnostics: nil,
		}),
		ToolResult: mo.None[agent.ToolResult](),
	}
}

// costStatisticsEntry creates one durable model entry with configured identity and persisted cost.
func costStatisticsEntry(
	providerID model.ProviderID,
	modelID model.ID,
	estimatedCost mo.Option[session.EstimatedCost],
) session.Entry {
	return session.Entry{ParentID: mo.None[string](), ID: string(providerID) + "/" + string(modelID), CreatedAt: time.Unix(10, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.Some(model.Response{
			Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.Some(providerID), Model: mo.Some(modelID), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.Some(model.Usage{}), Diagnostics: nil,
		}),
		EstimatedCost: estimatedCost, ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// costUsage builds normalized disjoint buckets and derives total without reasoning.
func costUsage(input, output, cacheRead, cacheWrite, reasoning int64) model.Usage {
	return model.Usage{
		InputTokens: input, OutputTokens: output, CachedInputTokens: cacheRead, CacheWriteTokens: cacheWrite,
		ReasoningTokens: reasoning, TotalTokens: input + output + cacheRead + cacheWrite,
	}
}

// expectedCost calculates expected values from the rate set selected by each scenario.
func expectedCost(usage model.Usage, rates model.PricingTier) session.EstimatedCost {
	const perMillion = 1_000_000
	cost := session.EstimatedCost{
		Input:      float64(usage.InputTokens) * rates.Input / perMillion,
		Output:     float64(usage.OutputTokens) * rates.Output / perMillion,
		CacheRead:  float64(usage.CachedInputTokens) * rates.CacheRead / perMillion,
		CacheWrite: float64(usage.CacheWriteTokens) * rates.CacheWrite / perMillion,
		Total:      0,
	}
	cost.Total = cost.Input + cost.Output + cost.CacheRead + cost.CacheWrite
	return cost
}
