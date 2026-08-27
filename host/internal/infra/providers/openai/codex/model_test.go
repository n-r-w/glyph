package codex

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestModelDescriptorAdvertisesKnownCapabilities verifies exact capability resolution for the approved model.
func TestModelDescriptorAdvertisesKnownCapabilities(t *testing.T) {
	t.Parallel()

	// Arrange the exact model ID with approved constrained-sampling capabilities.
	modelID := model.ID("gpt-5.6-luna")

	// Act by resolving its provider-owned descriptor.
	descriptor := ModelDescriptor(modelID)

	// Assert the known model advertises its exact capabilities and no pricing.
	assert.Equal(t, model.Descriptor{
		Provider: ProviderID,
		Model:    "gpt-5.6-luna",
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices: []model.ReasoningChoice{
				model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
				model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
				model.ReasoningChoiceMax,
			},
			Default: model.ReasoningChoiceMedium,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
		},
		Pricing: mo.None[model.Pricing](),
	}, descriptor)
}

// TestModelDescriptorDoesNotInferUnknownCapabilities verifies unknown models remain unsupported.
func TestModelDescriptorDoesNotInferUnknownCapabilities(t *testing.T) {
	t.Parallel()

	// Arrange a model ID that only shares a prefix with the known constrained model.
	modelID := model.ID("gpt-5.6-luna-preview")

	// Act by resolving its provider-owned descriptor.
	descriptor := ModelDescriptor(modelID)

	// Assert the unknown model keeps default tool capabilities and no pricing.
	assert.Equal(t, model.Descriptor{
		Provider: ProviderID,
		Model:    "gpt-5.6-luna-preview",
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices: []model.ReasoningChoice{
				model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
				model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
				model.ReasoningChoiceMax,
			},
			Default: model.ReasoningChoiceMedium,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
		}, Pricing: mo.None[model.Pricing](),
	}, descriptor)
}
