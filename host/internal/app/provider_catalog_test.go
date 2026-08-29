package app

import (
	"path/filepath"

	"testing"

	"github.com/samber/mo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"

	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
)

// TestNewProviderCatalogBuildsEveryConfiguredProvider verifies deterministic composition and defaults.
func TestNewProviderCatalogBuildsEveryConfiguredProvider(t *testing.T) {
	t.Parallel()

	// Arrange all supported provider types with deterministic identifiers and defaults.
	configured := settingstore.Settings{
		DefaultProvider: "a-compatible",
		DefaultModel:    "a-second",
		Providers: map[string]settingstore.Provider{
			"openai-codex": {
				Type: settingstore.ProviderTypeOpenAICodex,
				Models: []settingstore.Model{
					{
						ID: "gpt-5.6-luna", API: "",
						Input:         []model.InputModality{model.InputModalityText, model.InputModalityImage},
						ContextWindow: 200000, MaxTokens: 20000,
						ToolCapabilities: model.ToolCapabilities{
							StrictJSONSchema: false,
							Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
						},
						Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceOff),
						Pricing:   mo.None[model.Pricing](),
					},
					{
						ID: "codex-arbitrary", API: "", Input: []model.InputModality{model.InputModalityText},
						ContextWindow: 100000, MaxTokens: 10000,
						ToolCapabilities: model.ToolCapabilities{
							StrictJSONSchema: true,
							Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
						},
						Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceLow),
						Pricing:   mo.None[model.Pricing](),
					},
				},
				BaseURL: "",
				API:     "",
				APIKey:  mo.None[settingstore.APIKey](),
			},
			"z-compatible": {
				Type:    settingstore.ProviderTypeOpenAICompatible,
				BaseURL: "http://localhost:11434/v1",
				API:     settingstore.APIChatCompletions,
				Models: []settingstore.Model{{
					ID: "z-model",
					Reasoning: settingstore.Reasoning{
						Supported:        true,
						Choices:          []settingstore.ReasoningChoice{settingstore.ReasoningChoiceOn},
						Default:          settingstore.ReasoningChoiceOn,
						Format:           "openrouter",
						CompatibilityKey: mo.None[string](),
					},
					API: "", Input: []model.InputModality{model.InputModalityText},
					ContextWindow: 32000, MaxTokens: 4000,
					ToolCapabilities: model.ToolCapabilities{
						StrictJSONSchema: false,
						Grammar:          model.GrammarCapabilities{Lark: true, Regex: false},
					},
					Pricing: mo.None[model.Pricing](),
				}},
				APIKey: mo.None[settingstore.APIKey](),
			},
			"a-compatible": {
				Type:    settingstore.ProviderTypeOpenAICompatible,
				BaseURL: "https://example.com/v1",
				API:     settingstore.APIChatCompletions,
				APIKey: mo.Some(settingstore.APIKey{
					Literal:     mo.None[string](),
					Environment: mo.Some("COMPATIBLE_API_KEY"),
					Credential:  mo.None[string](),
				}),
				Models: []settingstore.Model{
					{
						ID: "a-first", API: "",
						Input:         []model.InputModality{model.InputModalityText, model.InputModalityImage},
						ContextWindow: 131072, MaxTokens: 16384,
						ToolCapabilities: model.ToolCapabilities{
							StrictJSONSchema: true,
							Grammar:          model.GrammarCapabilities{Lark: false, Regex: true},
						},
						Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceOff),
						Pricing:   mo.None[model.Pricing](),
					},
					{
						ID: "a-second", API: settingstore.APIResponses,
						Input:         []model.InputModality{model.InputModalityText},
						ContextWindow: 64000, MaxTokens: 8000,
						ToolCapabilities: model.ToolCapabilities{
							StrictJSONSchema: false,
							Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
						},
						Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceLow, settingstore.ReasoningChoiceHigh),
						Pricing:   mo.None[model.Pricing](),
					},
				},
			},
		},
		ActiveUI: mo.None[string](),
	}
	paths := persistence.Paths{
		CredentialsFile: filepath.Join(t.TempDir(), "credentials.json"),
		Directory:       "",
		SettingsFile:    "",
		LogsDirectory:   "",
		LogFile:         "",
	}

	// Act by building the shared Host provider catalog.
	catalog, err := newProviderCatalog(configured, paths, interactions.New(), hookrunner.New(nil, nil, nil))

	// Assert every configured model, order, and default selection are exact.
	require.NoError(t, err)
	models := catalog.Models()
	require.Len(t, models, 5)
	assert.Equal(t, []string{
		"a-compatible/a-first", "a-compatible/a-second", "openai-codex/gpt-5.6-luna",
		"openai-codex/codex-arbitrary", "z-compatible/z-model",
	}, []string{
		string(models[0].Provider) + "/" + string(models[0].Model),
		string(models[1].Provider) + "/" + string(models[1].Model),
		string(models[2].Provider) + "/" + string(models[2].Model),
		string(models[3].Provider) + "/" + string(models[3].Model),
		string(models[4].Provider) + "/" + string(models[4].Model),
	})
	assert.Equal(t, model.Selection{
		Provider:        "a-compatible",
		Model:           "a-second",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}, catalog.Selection())
	assert.Equal(t, model.ProviderID("a-compatible"), catalog.Current().Model.Provider)
	// Assert exact execution capabilities in catalog order.
	assert.Equal(t, []model.InputModality{model.InputModalityText, model.InputModalityImage}, models[0].Input)
	assert.Equal(t, int64(131072), models[0].ContextWindow)
	assert.Equal(t, int64(16384), models[0].MaxTokens)
	assert.Equal(t, []model.InputModality{model.InputModalityText}, models[1].Input)
	assert.Equal(t, int64(64000), models[1].ContextWindow)
	assert.Equal(t, int64(8000), models[1].MaxTokens)
	assert.Equal(t, []model.InputModality{model.InputModalityText, model.InputModalityImage}, models[2].Input)
	assert.Equal(t, int64(200000), models[2].ContextWindow)
	assert.Equal(t, int64(20000), models[2].MaxTokens)
	assert.Equal(t, []model.InputModality{model.InputModalityText}, models[3].Input)
	assert.Equal(t, int64(100000), models[3].ContextWindow)
	assert.Equal(t, int64(10000), models[3].MaxTokens)
	assert.Equal(t, []model.InputModality{model.InputModalityText}, models[4].Input)
	assert.Equal(t, int64(32000), models[4].ContextWindow)
	assert.Equal(t, int64(4000), models[4].MaxTokens)
	assert.Equal(t, model.ToolCapabilities{
		StrictJSONSchema: true,
		Grammar:          model.GrammarCapabilities{Lark: false, Regex: true},
	}, models[0].ToolCapabilities)
	assert.Equal(t, model.ToolCapabilities{
		StrictJSONSchema: false,
		Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
	}, models[1].ToolCapabilities)
	assert.Equal(t, model.ToolCapabilities{
		StrictJSONSchema: false,
		Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
	}, models[2].ToolCapabilities)
	assert.Equal(t, model.ToolCapabilities{
		StrictJSONSchema: true,
		Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
	}, models[3].ToolCapabilities)
	assert.Equal(t, model.ToolCapabilities{
		StrictJSONSchema: false,
		Grammar:          model.GrammarCapabilities{Lark: true, Regex: false},
	}, models[4].ToolCapabilities)
	assert.Equal(t, model.ReasoningCapabilities{
		Supported: true,
		Choices:   []model.ReasoningChoice{model.ReasoningChoiceOn},
		Default:   model.ReasoningChoiceOn,
	}, models[4].ReasoningCapabilities)
}
