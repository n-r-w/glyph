package settings

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestLoadParsesReasoningCapabilities verifies each capability shape and its explicit model default.
func (s *SettingsSuite) TestLoadParsesReasoningCapabilities() {
	path := writeSettings(s.T(), `defaultProvider: openrouter
defaultModel: effort
activeUI: "  Glyph__TUI--Plugin  "
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: codex
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [off, low, high]
          default: high
          compatibilityKey: gpt5
  openrouter:
    type: openai-compatible
    baseURL: https://openrouter.ai/api/v1
    api: responses
    apiKey:
      environment: OPENROUTER_API_KEY
    models:
      - id: effort
        input: [text, image]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [minimal, medium, high]
          default: medium
      - id: toggle
        input: [text]
        contextWindow: 65536
        maxTokens: 8192
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [off, on]
          default: on
      - id: fixed
        input: [text]
        contextWindow: 65536
        maxTokens: 8192
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [on]
          default: on
      - id: chat-reasoning-effort
        api: chat-completions
        input: [text]
        contextWindow: 65536
        maxTokens: 8192
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [off, low, high]
          default: low
          format: openai-chat
      - id: chat-reasoning-fixed
        api: chat-completions
        input: [text]
        contextWindow: 65536
        maxTokens: 8192
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [on]
          default: on
          format: openrouter
      - id: plain
        input: [text]
        contextWindow: 65536
        maxTokens: 8192
        toolCapabilities: {}
        reasoning:
          supported: false
          choices: [off]
          default: off
`)

	loaded, err := New(path).Load()

	s.Require().NoError(err)
	s.Equal("openrouter", loaded.DefaultProvider)
	s.Equal("effort", loaded.DefaultModel)
	s.Equal([]model.InputModality{model.InputModalityText, model.InputModalityImage}, loaded.Providers["openrouter"].Models[0].Input)
	s.Equal(int64(131072), loaded.Providers["openrouter"].Models[0].ContextWindow)
	s.Equal(int64(16384), loaded.Providers["openrouter"].Models[0].MaxTokens)
	s.Equal(mo.Some("glyph-tui-plugin"), loaded.ActiveUI)
	models := loaded.Providers["openrouter"].Models
	s.Require().Len(models, 6)
	s.Equal(Reasoning{
		Supported: true, Choices: []ReasoningChoice{ReasoningChoiceMinimal, ReasoningChoiceMedium, ReasoningChoiceHigh},
		Default: ReasoningChoiceMedium, CompatibilityKey: mo.None[string](), Format: "",
	}, models[0].Reasoning)
	s.Equal([]ReasoningChoice{ReasoningChoiceOff, ReasoningChoiceOn}, models[1].Reasoning.Choices)
	s.Equal(ReasoningChoiceOn, models[2].Reasoning.Default)
	s.Equal(Reasoning{
		Supported:        true,
		Choices:          []ReasoningChoice{ReasoningChoiceOff, ReasoningChoiceLow, ReasoningChoiceHigh},
		Default:          ReasoningChoiceLow,
		CompatibilityKey: mo.None[string](),
		Format:           "openai-chat",
	}, models[3].Reasoning)
	s.Equal(Reasoning{
		Supported: true, Choices: []ReasoningChoice{ReasoningChoiceOn}, Default: ReasoningChoiceOn,
		CompatibilityKey: mo.None[string](), Format: "openrouter",
	}, models[4].Reasoning)
	s.Equal(Reasoning{
		Supported: false, Choices: []ReasoningChoice{ReasoningChoiceOff}, Default: ReasoningChoiceOff,
		CompatibilityKey: mo.None[string](), Format: "",
	}, models[5].Reasoning)
}

// TestLoadParsesToolCapabilities verifies exact declarative capability mapping for an arbitrary model ID.
func (s *SettingsSuite) TestLoadParsesToolCapabilities() {
	// Arrange settings with all capabilities enabled for an arbitrary OpenAI-compatible model.
	path := writeSettings(s.T(), validSettings(""))

	// Act by loading the strict settings document.
	loaded, err := New(path).Load()

	// Assert all nested capability values are preserved exactly.
	s.Require().NoError(err)
	s.Equal(model.ToolCapabilities{
		StrictJSONSchema: true,
		Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
	}, loaded.Providers["compatible"].Models[0].ToolCapabilities)
	s.Equal(model.ToolCapabilities{
		StrictJSONSchema: false,
		Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
	}, loaded.Providers["compatible"].Models[1].ToolCapabilities)
}

// TestLoadAcceptsEachAPIKeySource verifies the structured union's three valid variants.
func (s *SettingsSuite) TestLoadAcceptsEachAPIKeySource() {
	testCases := map[string]struct {
		source   string
		expected APIKey
	}{
		"literal": {
			source: "literal: '!not-a-command'",
			expected: APIKey{
				Literal: mo.Some("!not-a-command"), Environment: mo.None[string](), Credential: mo.None[string](),
			},
		},
		"environment": {
			source: "environment: GLYPH_TEST_API_KEY",
			expected: APIKey{
				Literal: mo.None[string](), Environment: mo.Some("GLYPH_TEST_API_KEY"), Credential: mo.None[string](),
			},
		},
		"credential": {
			source: "credential: local-entry",
			expected: APIKey{
				Literal: mo.None[string](), Environment: mo.None[string](), Credential: mo.Some("local-entry"),
			},
		},
	}
	for name, testCase := range testCases {
		s.Run(name, func() {
			loaded, err := New(writeSettings(s.T(), validSettings("    apiKey:\n      "+testCase.source))).Load()
			s.Require().NoError(err)
			apiKey, present := loaded.Providers["compatible"].APIKey.Get()
			s.Require().True(present)
			s.Equal(testCase.expected, apiKey)
		})
	}
}
