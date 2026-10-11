package runtime

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapCompactionModel projects every provider-neutral descriptor field.
func mapCompactionModel(descriptor model.Descriptor) *extensionpb.ModelDescriptor {
	modalities := make([]extensionpb.InputModality, len(descriptor.Input))
	for index, input := range descriptor.Input {
		switch input {
		case model.InputModalityText:
			modalities[index] = extensionpb.InputModality_INPUT_MODALITY_TEXT
		case model.InputModalityImage:
			modalities[index] = extensionpb.InputModality_INPUT_MODALITY_IMAGE
		}
	}
	choices := make([]string, len(descriptor.ReasoningCapabilities.Choices))
	for index, choice := range descriptor.ReasoningCapabilities.Choices {
		choices[index] = string(choice)
	}
	var pricing *extensionpb.ModelPricing
	if configured, present := descriptor.Pricing.Get(); present {
		tiers := make([]*extensionpb.PricingTier, len(configured.Tiers))
		for index, tier := range configured.Tiers {
			tiers[index] = extensionpb.PricingTier_builder{
				InputTokensAbove: new(tier.InputTokensAbove), Input: new(tier.Input), Output: new(tier.Output),
				CacheRead: new(tier.CacheRead), CacheWrite: new(tier.CacheWrite),
			}.Build()
		}
		pricing = extensionpb.ModelPricing_builder{
			Input: new(configured.Input), Output: new(configured.Output), CacheRead: new(configured.CacheRead),
			CacheWrite: new(configured.CacheWrite), Tiers: tiers,
		}.Build()
	}
	return extensionpb.ModelDescriptor_builder{
		ProviderId: new(string(descriptor.Provider)), ModelId: new(string(descriptor.Model)),
		InputModalities: modalities, ContextWindow: new(descriptor.ContextWindow), MaxTokens: new(descriptor.MaxTokens),
		Reasoning: extensionpb.ReasoningCapabilities_builder{
			Supported: new(descriptor.ReasoningCapabilities.Supported), Choices: choices,
			DefaultChoice: new(string(descriptor.ReasoningCapabilities.Default)),
		}.Build(),
		Tools: extensionpb.ToolCapabilities_builder{
			StrictJsonSchema: new(descriptor.ToolCapabilities.StrictJSONSchema),
			Lark: new(
				descriptor.ToolCapabilities.Grammar.Lark,
			),
			Regex: new(descriptor.ToolCapabilities.Grammar.Regex),
		}.Build(),
		Pricing: pricing,
	}.Build()
}
