package extension

import (
	"slices"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// decodeModel maps a public model descriptor into policy data.
func decodeModel(value *pb.ModelDescriptor) compaction.Model {
	model := compaction.Model{
		Provider:   value.GetProviderId(),
		ID:         value.GetModelId(),
		Window:     value.GetContextWindow(),
		Reserve:    value.GetMaxTokens(),
		Modalities: nil,
		Reasoning:  mo.None[compaction.Reasoning](),
		Tools:      mo.None[compaction.ToolCapabilities](),
		Pricing:    mo.None[compaction.Pricing](),
	}
	for _, modality := range value.GetInputModalities() {
		model.Modalities = append(model.Modalities, decodeModality(modality))
	}
	if reasoning := value.GetReasoning(); reasoning != nil {
		model.Reasoning = mo.Some(
			compaction.Reasoning{
				Supported: reasoning.GetSupported(),
				Choices:   slices.Clone(reasoning.GetChoices()),
				Default:   reasoning.GetDefaultChoice(),
			},
		)
	}
	if tools := value.GetTools(); tools != nil {
		model.Tools = mo.Some(
			compaction.ToolCapabilities{
				Strict: tools.GetStrictJsonSchema(),
				Lark:   tools.GetLark(),
				Regex:  tools.GetRegex(),
			},
		)
	}
	if pricing := value.GetPricing(); pricing != nil {
		rates := compaction.Pricing{
			Rates: [4]float64{pricing.GetInput(), pricing.GetOutput(), pricing.GetCacheRead(), pricing.GetCacheWrite()},
			Tiers: nil,
		}
		for _, tier := range pricing.GetTiers() {
			rates.Tiers = append(
				rates.Tiers,
				compaction.Tier{
					Above: tier.GetInputTokensAbove(),
					Rates: [4]float64{tier.GetInput(), tier.GetOutput(), tier.GetCacheRead(), tier.GetCacheWrite()},
				},
			)
		}
		model.Pricing = mo.Some(rates)
	}
	return model
}

// encodeModel maps the composed model descriptor to the public contract.
func encodeModel(value compaction.Model) *pb.ModelDescriptor {
	model := new(pb.ModelDescriptor)
	model.SetProviderId(value.Provider)
	model.SetModelId(value.ID)
	model.SetContextWindow(value.Window)
	model.SetMaxTokens(value.Reserve)
	for _, modality := range value.Modalities {
		model.SetInputModalities(append(model.GetInputModalities(), encodeModality(modality)))
	}
	if reasoning, present := value.Reasoning.Get(); present {
		model.SetReasoning(
			pb.ReasoningCapabilities_builder{
				Supported:     new(reasoning.Supported),
				Choices:       slices.Clone(reasoning.Choices),
				DefaultChoice: new(reasoning.Default),
			}.Build(),
		)
	}
	if tools, present := value.Tools.Get(); present {
		model.SetTools(
			pb.ToolCapabilities_builder{
				StrictJsonSchema: new(tools.Strict),
				Lark:             new(tools.Lark),
				Regex:            new(tools.Regex),
			}.Build(),
		)
	}
	if pricing, present := value.Pricing.Get(); present {
		encoded := pb.ModelPricing_builder{
			Input:      new(pricing.Rates[0]),
			Output:     new(pricing.Rates[1]),
			CacheRead:  new(pricing.Rates[2]),
			CacheWrite: new(pricing.Rates[3]),
			Tiers:      nil,
		}.Build()
		for _, tier := range pricing.Tiers {
			encoded.SetTiers(
				append(
					encoded.GetTiers(),
					pb.PricingTier_builder{
						InputTokensAbove: new(tier.Above),
						Input:            new(tier.Rates[0]),
						Output:           new(tier.Rates[1]),
						CacheRead:        new(tier.Rates[2]),
						CacheWrite:       new(tier.Rates[3]),
					}.Build(),
				),
			)
		}
		model.SetPricing(encoded)
	}
	return model
}

// DecodeUsage maps normalized model usage into policy data.
func DecodeUsage(value *pb.ConfiguredModelUsage) mo.Option[compaction.Usage] {
	if value == nil {
		return mo.None[compaction.Usage]()
	}
	return mo.Some(
		compaction.Usage{
			Counts: [6]int64{
				value.GetInputTokens(),
				value.GetOutputTokens(),
				value.GetCachedInputTokens(),
				value.GetCacheWriteTokens(),
				value.GetReasoningTokens(),
				value.GetTotalTokens(),
			},
		},
	)
}

// decodeSavedUsage maps saved token usage into policy data.
func decodeSavedUsage(value *pb.TokenUsage) mo.Option[compaction.Usage] {
	if value == nil {
		return mo.None[compaction.Usage]()
	}
	return mo.Some(
		compaction.Usage{
			Counts: [6]int64{
				value.GetInputTokens(),
				value.GetOutputTokens(),
				value.GetCacheReadTokens(),
				value.GetCacheWriteTokens(),
				value.GetReasoningTokens(),
				value.GetTotalTokens(),
			},
		},
	)
}

// encodeUsage maps normalized accounting to the compaction contract.
func encodeUsage(value compaction.Usage) *pb.TokenUsage {
	return pb.TokenUsage_builder{
		InputTokens:      new(value.Counts[0]),
		OutputTokens:     new(value.Counts[1]),
		CacheReadTokens:  new(value.Counts[2]),
		CacheWriteTokens: new(value.Counts[3]),
		ReasoningTokens:  new(value.Counts[4]),
		TotalTokens:      new(value.Counts[5]),
	}.Build()
}
