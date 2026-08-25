package codex

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestModelDescriptorAdvertisesKnownCapabilities verifies exact capability resolution for the approved model.
func TestModelDescriptorAdvertisesKnownCapabilities(t *testing.T) {
	t.Parallel()

	descriptor := ModelDescriptor("gpt-5.6-luna")

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
	}, descriptor)
}

// TestModelDescriptorDoesNotInferUnknownCapabilities verifies unknown models remain unsupported.
func TestModelDescriptorDoesNotInferUnknownCapabilities(t *testing.T) {
	t.Parallel()

	descriptor := ModelDescriptor("gpt-5.6-luna-preview")

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
		},
	}, descriptor)
}
