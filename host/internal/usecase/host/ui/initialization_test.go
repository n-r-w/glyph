//go:build !integration

package ui

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestBuildInitializationUsesSharedModelCatalog verifies ordered models and active selection.
func TestBuildInitializationUsesSharedModelCatalog(t *testing.T) {
	t.Parallel()

	// Arrange a shared catalog with supported reasoning choices and one selection.
	catalog := NewMockModelCatalog(gomock.NewController(t))
	catalog.EXPECT().Models().Return([]model.Descriptor{{
		Provider: "openai-codex",
		Model:    "gpt",
		Input:    nil, ContextWindow: 0, MaxTokens: 0,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices: []model.ReasoningChoice{
				model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
				model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
				model.ReasoningChoiceMax,
			},
			Default: model.ReasoningChoiceHigh,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}, {
		Provider: "ollama",
		Model:    "ornith",
		Input:    nil, ContextWindow: 0, MaxTokens: 0,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOn},
			Default:   model.ReasoningChoiceOn,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}})
	catalog.EXPECT().ActiveSelection().Return(model.Selection{
		Provider:        "openai-codex",
		Model:           "gpt",
		ReasoningChoice: model.ReasoningChoiceHigh,
	})

	// Act by building the UI initialization snapshot.
	output := NewMockOutput(gomock.NewController(t))
	active := NewMockActiveSessions(gomock.NewController(t))
	active.EXPECT().ActiveInformation().Return(session.Info{}, session.Statistics{})
	var initialization Initialization
	output.EXPECT().
		Initialize(t.Context(), gomock.Any()).
		DoAndReturn(func(_ context.Context, state Initialization) error { initialization = state; return nil })
	service := NewSession(output, nil, nil, catalog, active, nil, nil, nil)
	require.NoError(t, service.Initialize(t.Context()))

	// Assert all catalog models and the active selection are mapped exactly.
	require.Len(t, initialization.Models, 2)
	assert.Equal(t, model.ProviderID("openai-codex"), initialization.Models[0].Provider)
	assert.Equal(t, model.ID("gpt"), initialization.Models[0].Model)
	assert.Equal(t, []model.ReasoningChoice{
		model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
		model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
		model.ReasoningChoiceMax,
	}, initialization.Models[0].ReasoningCapabilities.Choices)
	assert.True(t, initialization.Models[0].ReasoningCapabilities.Supported)
	assert.Equal(t, model.ReasoningChoiceHigh, initialization.Models[0].ReasoningCapabilities.Default)
	assert.Equal(t, model.ProviderID("ollama"), initialization.Models[1].Provider)
	assert.Equal(t, model.ID("ornith"), initialization.Models[1].Model)
	assert.Equal(
		t,
		[]model.ReasoningChoice{model.ReasoningChoiceOn},
		initialization.Models[1].ReasoningCapabilities.Choices,
	)
	assert.True(t, initialization.Models[1].ReasoningCapabilities.Supported)
	assert.Equal(t, model.ReasoningChoiceOn, initialization.Models[1].ReasoningCapabilities.Default)
	assert.Equal(t, model.Selection{
		Provider:        "openai-codex",
		Model:           "gpt",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}, initialization.ModelSelection.MustGet())
}
