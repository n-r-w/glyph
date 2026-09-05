package sessions

import (
	"sort"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// sessionEntryCounts owns shared public-message counts for durable session entries.
type sessionEntryCounts struct {
	// userMessages counts durable user entries.
	userMessages int
	// modelResponses counts durable terminal model entries.
	modelResponses int
	// toolResults counts durable terminal tool result entries.
	toolResults int
	// totalMessages counts all public terminal messages.
	totalMessages int
}

// add owns which durable entry kinds count as public messages.
func (counts *sessionEntryCounts) add(entry session.Entry) {
	if entry.User.IsSome() {
		counts.userMessages++
		counts.totalMessages++
	}
	if entry.Model.IsSome() {
		counts.modelResponses++
		counts.totalMessages++
	}
	if entry.ToolResult.IsSome() {
		counts.toolResults++
		counts.totalMessages++
	}
}

// countSessionEntries applies the shared public-message count rule to stored entries.
func countSessionEntries(entries []session.Entry) sessionEntryCounts {
	counts := sessionEntryCounts{
		userMessages: 0, modelResponses: 0, toolResults: 0, totalMessages: 0,
	}
	for entryIndex := range entries {
		counts.add(entries[entryIndex])
	}
	return counts
}

type providerModelKey struct {
	// provider identifies the configured model provider.
	provider model.ProviderID
	// model identifies the requested provider model.
	model model.ID
}

type accumulatedCost struct {
	// value contains accumulated disjoint cost buckets.
	value session.EstimatedCost
	// available reports whether every response had persisted cost.
	available bool
}

// statisticsFromEntries owns tool-call totals and complete token and cost availability.
func statisticsFromEntries(entries []session.Entry) session.Statistics {
	counts := countSessionEntries(entries)
	statistics := session.Statistics{
		UserMessages: counts.userMessages, ModelResponses: counts.modelResponses,
		ToolCalls: 0, ToolResults: counts.toolResults, TotalMessages: counts.totalMessages,
		TokenUsage: mo.Some(session.TokenUsage{}), EstimatedCost: mo.Some(session.EstimatedCost{}),
		CostBreakdown: nil,
	}
	usage := session.TokenUsage{}
	aggregateCost := accumulatedCost{value: session.EstimatedCost{}, available: true}
	groupCosts := make(map[providerModelKey]accumulatedCost)
	for entryIndex := range entries {
		entry := &entries[entryIndex]
		if response, present := entry.Model.Get(); present {
			for contentIndex := range response.Content {
				content := &response.Content[contentIndex]
				if content.Kind == model.ContentToolCall && content.ToolCall.IsSome() {
					statistics.ToolCalls++
				}
			}
			modelUsage, usagePresent := response.Usage.Get()
			if !usagePresent {
				// One absent model usage makes only token totals unavailable. Counts remain complete.
				statistics.TokenUsage = mo.None[session.TokenUsage]()
			} else {
				usage = usage.Add(session.TokenUsage{
					InputTokens: modelUsage.InputTokens, OutputTokens: modelUsage.OutputTokens,
					CacheReadTokens: modelUsage.CachedInputTokens, CacheWriteTokens: modelUsage.CacheWriteTokens,
					ReasoningTokens: modelUsage.ReasoningTokens, TotalTokens: modelUsage.TotalTokens,
				})
			}
			accumulateCost(
				response.Provider, response.Model, entry.EstimatedCost, &aggregateCost, groupCosts,
			)
		}
		if summary, present := entry.BranchSummary.Get(); present {
			// Extension-only summaries do not make otherwise complete model totals unavailable.
			modelSource, modelPresent := summary.Source.Model.Get()
			if !modelPresent {
				continue
			}
			summaryUsage, usagePresent := modelSource.Usage.Get()
			if !usagePresent {
				statistics.TokenUsage = mo.None[session.TokenUsage]()
			} else {
				usage = usage.Add(summaryUsage)
			}
			accumulateCost(
				mo.Some(modelSource.Selection.Provider), mo.Some(modelSource.Selection.Model), summary.EstimatedCost,
				&aggregateCost, groupCosts,
			)
		}
	}
	if statistics.TokenUsage.IsSome() {
		statistics.TokenUsage = mo.Some(usage)
	}
	if aggregateCost.available {
		statistics.EstimatedCost = mo.Some(aggregateCost.value)
	} else {
		statistics.EstimatedCost = mo.None[session.EstimatedCost]()
	}
	statistics.CostBreakdown = costBreakdown(groupCosts)
	return statistics
}

// accumulateCost keeps aggregate and exact provider-model availability independent.
func accumulateCost(
	provider mo.Option[model.ProviderID],
	modelID mo.Option[model.ID],
	storedCost mo.Option[session.EstimatedCost],
	aggregate *accumulatedCost,
	groups map[providerModelKey]accumulatedCost,
) {
	cost, costPresent := storedCost.Get()
	if !costPresent {
		aggregate.available = false
	} else if aggregate.available {
		aggregate.value = aggregate.value.Add(cost)
	}
	providerID, providerPresent := provider.Get()
	requestedModelID, modelPresent := modelID.Get()
	if !providerPresent || !modelPresent {
		return
	}
	key := providerModelKey{provider: providerID, model: requestedModelID}
	group, found := groups[key]
	if !found {
		group = accumulatedCost{value: session.EstimatedCost{}, available: true}
	}
	if !costPresent {
		group.available = false
	} else if group.available {
		group.value = group.value.Add(cost)
	}
	groups[key] = group
}

// costBreakdown maps sorted exact provider-model keys into public availability values.
func costBreakdown(groups map[providerModelKey]accumulatedCost) []session.ProviderModelCost {
	if len(groups) == 0 {
		return nil
	}
	breakdown := make([]session.ProviderModelCost, 0, len(groups))
	for key, group := range groups {
		cost := mo.None[session.EstimatedCost]()
		if group.available {
			cost = mo.Some(group.value)
		}
		breakdown = append(breakdown, session.ProviderModelCost{
			Provider: key.provider, Model: key.model, EstimatedCost: cost,
		})
	}
	sort.Slice(breakdown, func(left, right int) bool {
		if breakdown[left].Provider != breakdown[right].Provider {
			return breakdown[left].Provider < breakdown[right].Provider
		}
		return breakdown[left].Model < breakdown[right].Model
	})
	return breakdown
}
