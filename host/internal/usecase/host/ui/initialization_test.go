//nolint:exhaustruct // Tests set only fields relevant to initialization behavior.
package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// TestBuildInitializationIncludesFailuresAvailabilityAndOneSummary verifies startup delivery content.
func TestBuildInitializationIncludesFailuresAvailabilityAndOneSummary(t *testing.T) {
	t.Parallel()

	initialization := BuildInitialization("selected", toolservice.LoadReport{
		Issues: []toolservice.Issue{{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("failed")}},
		Extensions: []toolservice.LoadedExtension{{
			ID: "tools", Path: "/plugins/tools",
			Tools: []tool.Descriptor{{Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`)}},
		}},
	}, []SelectionIssue{{
		Candidate: domainui.Candidate{ID: "excluded", Path: "/excluded"}, Err: errors.New("incompatible"),
	}}, testModelCatalog(t))

	assert.Equal(t, "selected", initialization.SelectedUIID)
	assert.Equal(t, domainui.AvailabilityCheckingAuthentication, initialization.Availability)
	require.Len(t, initialization.StartupContent, 3)
	assert.Equal(t, domainui.ContentSeverityError, initialization.StartupContent[0].Severity)
	assert.Contains(t, initialization.StartupContent[0].Text, "broken")
	assert.Contains(t, initialization.StartupContent[0].Text, "/broken")
	assert.Equal(t, domainui.ContentSeverityWarning, initialization.StartupContent[1].Severity)
	assert.Contains(t, initialization.StartupContent[1].Text, "excluded")
	assert.Contains(t, initialization.StartupContent[1].Text, "/excluded")
	assert.Equal(t, domainui.ContentSeverityInformation, initialization.StartupContent[2].Severity)
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

	catalog := NewMockModelCatalog(gomock.NewController(t))
	catalog.EXPECT().Models().Return([]model.Descriptor{{
		Provider: "openai-codex", Model: "gpt",
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices: []model.ReasoningChoice{
				model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
				model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
				model.ReasoningChoiceMax,
			},
			Default: model.ReasoningChoiceHigh,
		},
	}, {
		Provider: "ollama", Model: "ornith",
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceOn},
			Default: model.ReasoningChoiceOn,
		},
	}})
	catalog.EXPECT().Selection().Return(model.Selection{
		Provider: "openai-codex", Model: "gpt", ReasoningChoice: model.ReasoningChoiceHigh,
	})

	initialization := BuildInitialization("selected", toolservice.LoadReport{}, nil, catalog)

	require.Len(t, initialization.Models, 2)
	assert.Equal(t, "openai-codex", initialization.Models[0].ProviderID)
	assert.Equal(t, "gpt", initialization.Models[0].ModelID)
	assert.Equal(t, []domainui.ReasoningChoice{
		domainui.ReasoningChoiceOff, domainui.ReasoningChoiceMinimal, domainui.ReasoningChoiceLow,
		domainui.ReasoningChoiceMedium, domainui.ReasoningChoiceHigh, domainui.ReasoningChoiceXHigh,
		domainui.ReasoningChoiceMax,
	}, initialization.Models[0].Reasoning.Choices)
	assert.True(t, initialization.Models[0].Reasoning.Supported)
	assert.Equal(t, domainui.ReasoningChoiceHigh, initialization.Models[0].Reasoning.Default)
	assert.Equal(t, "ollama", initialization.Models[1].ProviderID)
	assert.Equal(t, "ornith", initialization.Models[1].ModelID)
	assert.Equal(t, []domainui.ReasoningChoice{domainui.ReasoningChoiceOn}, initialization.Models[1].Reasoning.Choices)
	assert.True(t, initialization.Models[1].Reasoning.Supported)
	assert.Equal(t, domainui.ReasoningChoiceOn, initialization.Models[1].Reasoning.Default)
	assert.Equal(t, domainui.ModelSelection{
		ProviderID: "openai-codex", ModelID: "gpt", ReasoningChoice: domainui.ReasoningChoiceHigh,
	}, initialization.ModelSelection)
}

// TestBuildInitializationTreatsEmptyExtensionsAsNormalInformation verifies empty catalogs are not errors.
func TestBuildInitializationTreatsEmptyExtensionsAsNormalInformation(t *testing.T) {
	t.Parallel()

	initialization := BuildInitialization("selected", toolservice.LoadReport{
		Issues: nil, Extensions: nil,
	}, nil, testModelCatalog(t))

	require.Len(t, initialization.StartupContent, 1)
	assert.Equal(t, domainui.ContentSeverityInformation, initialization.StartupContent[0].Severity)
	assert.Contains(t, initialization.StartupContent[0].Text, "extensions: none")
	assert.Empty(t, initialization.Extensions)
}

// testModelCatalog returns one valid catalog for initialization content tests.
func testModelCatalog(t *testing.T) ModelCatalog {
	t.Helper()
	catalog := NewMockModelCatalog(gomock.NewController(t))
	catalog.EXPECT().Models().Return([]model.Descriptor{{
		Provider: "openai-codex", Model: "gpt",
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceHigh},
			Default: model.ReasoningChoiceHigh,
		},
	}})
	catalog.EXPECT().Selection().Return(model.Selection{
		Provider: "openai-codex", Model: "gpt", ReasoningChoice: model.ReasoningChoiceHigh,
	})
	return catalog
}
