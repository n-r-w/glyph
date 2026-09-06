//go:build !integration

package ui

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestBuildInitializationIncludesFailuresAvailabilityAndOneSummary verifies startup delivery content.
func TestBuildInitializationIncludesFailuresAvailabilityAndOneSummary(t *testing.T) {
	t.Parallel()

	initialization := BuildInitialization("selected", ExtensionLoadReport{
		Issues: []ExtensionLoadIssue{{
			PluginIDs: []string{"broken"},
			Path:      "/broken",
			Err:       errors.New("failed"),
		}},
		Extensions: []LoadedExtension{{
			ID: "tools", Path: "/plugins/tools", Tools: []string{"read"},
		}},
	}, []SelectionIssue{{
		Candidate: Candidate{
			ID:   "excluded",
			Path: "/excluded",
		},
		Err: errors.New("incompatible"),
	}}, testModelCatalog(t))

	assert.Equal(t, "selected", initialization.SelectedUIID)
	assert.Equal(t, AvailabilityCheckingAuthentication, initialization.Availability)
	require.Len(t, initialization.StartupContent, 3)
	assert.Equal(t, ContentSeverityError, initialization.StartupContent[0].Severity)
	assert.Contains(t, initialization.StartupContent[0].Text, "broken")
	assert.Contains(t, initialization.StartupContent[0].Text, "/broken")
	assert.Equal(t, ContentSeverityWarning, initialization.StartupContent[1].Severity)
	assert.Contains(t, initialization.StartupContent[1].Text, "excluded")
	assert.Contains(t, initialization.StartupContent[1].Text, "/excluded")
	assert.Equal(t, ContentSeverityInformation, initialization.StartupContent[2].Severity)
	assert.Contains(t, initialization.StartupContent[2].Text, "UI selected")
	assert.Contains(t, initialization.StartupContent[2].Text, "/plugins/tools")
	require.Len(t, initialization.Extensions, 1)
	assert.Equal(t, "tools", initialization.Extensions[0].PluginID)
	assert.Equal(t, "/plugins/tools", initialization.Extensions[0].Path)
	assert.Equal(t, []string{"read"}, initialization.Extensions[0].Tools)
}

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
	initialization := BuildInitialization("selected", ExtensionLoadReport{Issues: nil, Extensions: nil}, nil, catalog)

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

// TestBuildInitializationTreatsEmptyExtensionsAsNormalInformation verifies empty catalogs are not errors.
func TestBuildInitializationTreatsEmptyExtensionsAsNormalInformation(t *testing.T) {
	t.Parallel()

	initialization := BuildInitialization("selected", ExtensionLoadReport{
		Issues: nil, Extensions: nil,
	}, nil, testModelCatalog(t))

	require.Len(t, initialization.StartupContent, 1)
	assert.Equal(t, ContentSeverityInformation, initialization.StartupContent[0].Severity)
	assert.Contains(t, initialization.StartupContent[0].Text, "extensions: none")
	assert.Empty(t, initialization.Extensions)
}

// testModelCatalog returns one valid catalog for initialization content tests.
func testModelCatalog(t *testing.T) ModelCatalog {
	t.Helper()
	catalog := NewMockModelCatalog(gomock.NewController(t))
	catalog.EXPECT().Models().Return([]model.Descriptor{{
		Provider: "openai-codex",
		Model:    "gpt",
		Input:    nil, ContextWindow: 0, MaxTokens: 0,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceHigh},
			Default:   model.ReasoningChoiceHigh,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}})
	catalog.EXPECT().ActiveSelection().Return(model.Selection{
		Provider:        "openai-codex",
		Model:           "gpt",
		ReasoningChoice: model.ReasoningChoiceHigh,
	})
	return catalog
}
