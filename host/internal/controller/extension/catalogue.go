package extension

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapModelCatalog projects all neutral descriptor fields and active selection without provider-only state.
func mapModelCatalog(catalog ModelCatalog) *extensionpb.GetModelsResult {
	models := make([]*extensionpb.ModelDescriptor, len(catalog.Models))
	for index := range catalog.Models {
		models[index] = mapDescriptor(catalog.Models[index])
	}
	return extensionpb.GetModelsResult_builder{Models: models, ActiveSelection: extensionpb.ModelSelection_builder{
		ProviderId: new(
			string(catalog.Selection.Provider),
		),
		ModelId:         new(string(catalog.Selection.Model)),
		ReasoningChoice: new(string(catalog.Selection.ReasoningChoice)),
	}.Build()}.Build()
}

// mapDescriptor maps only domain-owned model capabilities and optional pricing.
func mapDescriptor(descriptor model.Descriptor) *extensionpb.ModelDescriptor {
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
		pricing = mapPricing(configured)
	}
	return extensionpb.ModelDescriptor_builder{
		ProviderId: new(
			string(descriptor.Provider),
		),
		ModelId:         new(string(descriptor.Model)),
		InputModalities: modalities,
		ContextWindow:   new(descriptor.ContextWindow),
		MaxTokens:       new(descriptor.MaxTokens),
		Reasoning: extensionpb.ReasoningCapabilities_builder{
			Supported:     new(descriptor.ReasoningCapabilities.Supported),
			Choices:       choices,
			DefaultChoice: new(string(descriptor.ReasoningCapabilities.Default)),
		}.Build(),
		Tools: extensionpb.ToolCapabilities_builder{
			StrictJsonSchema: new(descriptor.ToolCapabilities.StrictJSONSchema),
			Lark:             new(descriptor.ToolCapabilities.Grammar.Lark),
			Regex:            new(descriptor.ToolCapabilities.Grammar.Regex),
		}.Build(),
		Pricing: pricing,
	}.Build()
}

// mapPricing preserves configured fractional USD rates and ordered threshold overrides.
func mapPricing(pricing model.Pricing) *extensionpb.ModelPricing {
	tiers := make([]*extensionpb.PricingTier, len(pricing.Tiers))
	for index, tier := range pricing.Tiers {
		tiers[index] = extensionpb.PricingTier_builder{
			InputTokensAbove: new(tier.InputTokensAbove),
			Input:            new(tier.Input),
			Output:           new(tier.Output),
			CacheRead:        new(tier.CacheRead),
			CacheWrite:       new(tier.CacheWrite),
		}.Build()
	}
	return extensionpb.ModelPricing_builder{
		Input:      new(pricing.Input),
		Output:     new(pricing.Output),
		CacheRead:  new(pricing.CacheRead),
		CacheWrite: new(pricing.CacheWrite),
		Tiers:      tiers,
	}.Build()
}

// mapProviderCatalog exposes provider IDs and ordered model IDs without configuration fields.
func mapProviderCatalog(providers []Provider) *extensionpb.GetProvidersResult {
	mapped := make([]*extensionpb.ProviderDescriptor, len(providers))
	for index, provider := range providers {
		ids := make([]string, len(provider.ModelIDs))
		for modelIndex, id := range provider.ModelIDs {
			ids[modelIndex] = string(id)
		}
		mapped[index] = extensionpb.ProviderDescriptor_builder{
			ProviderId: new(string(provider.ID)),
			ModelIds:   ids,
		}.Build()
	}
	return extensionpb.GetProvidersResult_builder{Providers: mapped}.Build()
}
