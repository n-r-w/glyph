package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.yaml.in/yaml/v3"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

type SettingsSuite struct{ suite.Suite }

func TestSettingsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SettingsSuite))
}

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
        reasoning:
          supported: true
          choices: [off, low, high]
          default: high
          compatibilityKey: gpt5
          wireFormat: openai-responses
  openrouter:
    type: openai-compatible
    baseURL: https://openrouter.ai/api/v1
    api: responses
    apiKey:
      environment: OPENROUTER_API_KEY
    models:
      - id: effort
        reasoning:
          supported: true
          choices: [minimal, medium, high]
          default: medium
          wireFormat: openai-responses
      - id: toggle
        reasoning:
          supported: true
          choices: [off, on]
          default: on
          wireFormat: openai-responses
      - id: fixed
        reasoning:
          supported: true
          choices: [on]
          default: on
          wireFormat: openai-responses
      - id: chat-effort
        api: chat-completions
        reasoning:
          supported: true
          choices: [off, low, high]
          default: low
          wireFormat: openai-chat-effort
      - id: ornith
        api: chat-completions
        reasoning:
          supported: true
          choices: [on]
          default: on
          wireFormat: ollama-ornith
      - id: plain
        reasoning:
          supported: false
          choices: [off]
          default: off
`)

	loaded, err := New(path).Load()

	s.Require().NoError(err)
	s.Equal("openrouter", loaded.DefaultProvider)
	s.Equal("effort", loaded.DefaultModel)
	s.Equal(mo.Some("glyph-tui-plugin"), loaded.ActiveUI)
	models := loaded.Providers["openrouter"].Models
	s.Require().Len(models, 6)
	s.Equal(Reasoning{
		Supported: true, Choices: []ReasoningChoice{ReasoningChoiceMinimal, ReasoningChoiceMedium, ReasoningChoiceHigh},
		Default: ReasoningChoiceMedium, CompatibilityKey: mo.None[string](), WireFormat: ReasoningWireFormatOpenAIResponses,
	}, models[0].Reasoning)
	s.Equal([]ReasoningChoice{ReasoningChoiceOff, ReasoningChoiceOn}, models[1].Reasoning.Choices)
	s.Equal(ReasoningChoiceOn, models[2].Reasoning.Default)
	s.Equal(Reasoning{
		Supported:        true,
		Choices:          []ReasoningChoice{ReasoningChoiceOff, ReasoningChoiceLow, ReasoningChoiceHigh},
		Default:          ReasoningChoiceLow,
		CompatibilityKey: mo.None[string](),
		WireFormat:       ReasoningWireFormatOpenAIChatEffort,
	}, models[3].Reasoning)
	s.Equal(Reasoning{
		Supported: true, Choices: []ReasoningChoice{ReasoningChoiceOn}, Default: ReasoningChoiceOn,
		CompatibilityKey: mo.None[string](), WireFormat: ReasoningWireFormatOllamaOrnith,
	}, models[4].Reasoning)
	s.Equal(Reasoning{
		Supported: false, Choices: []ReasoningChoice{ReasoningChoiceOff}, Default: ReasoningChoiceOff,
		CompatibilityKey: mo.None[string](), WireFormat: "",
	}, models[5].Reasoning)
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

// TestLoadPreservesOptionalScalarYAML verifies boundary conversion for omitted, null, empty, and non-empty scalars.
func (s *SettingsSuite) TestLoadPreservesOptionalScalarYAML() {
	s.Run("omitted", func() {
		content := validSettings("")
		decoded := decodeSettingsFile(s.T(), content)
		s.True(decoded.ActiveUI.IsNone())
		s.True(decoded.Providers["compatible"].APIKey.IsNone())
		s.True(decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())

		loaded, err := New(writeSettings(s.T(), content)).Load()
		s.Require().NoError(err)
		s.True(loaded.ActiveUI.IsNone())
		s.True(loaded.Providers["compatible"].APIKey.IsNone())
		s.True(loaded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())
	})
	s.Run("null", func() {
		content := replace(validSettings(""), "providers:", "activeUI: null\nproviders:")
		content = replace(content, "    baseURL: https://example.com/v1", "    baseURL: https://example.com/v1\n    apiKey: null")
		content = replace(content, "          wireFormat: openai-responses\n      - id: plain", "          compatibilityKey: null\n          wireFormat: openai-responses\n      - id: plain")
		decoded := decodeSettingsFile(s.T(), content)
		s.True(decoded.ActiveUI.IsNone())
		s.True(decoded.Providers["compatible"].APIKey.IsNone())
		s.True(decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())

		loaded, err := New(writeSettings(s.T(), content)).Load()
		s.Require().NoError(err)
		s.True(loaded.ActiveUI.IsNone())
		s.True(loaded.Providers["compatible"].APIKey.IsNone())
		s.True(loaded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())
	})
	s.Run("empty", func() {
		testCases := map[string]struct {
			content string
			assert  func(settingsFile)
		}{
			"active UI": {
				content: replace(validSettings(""), "providers:", "activeUI: ''\nproviders:"),
				assert:  func(decoded settingsFile) { s.Equal(mo.Some(""), decoded.ActiveUI) },
			},
			"API key source": {
				content: replace(validSettings(""), "    baseURL: https://example.com/v1", "    baseURL: https://example.com/v1\n    apiKey:\n      literal: ''"),
				assert: func(decoded settingsFile) {
					apiKey, present := decoded.Providers["compatible"].APIKey.Get()
					s.Require().True(present)
					s.Equal(mo.Some(""), apiKey.Literal)
				},
			},
			"compatibility key": {
				content: replace(validSettings(""), "          wireFormat: openai-responses\n      - id: plain", "          compatibilityKey: ''\n          wireFormat: openai-responses\n      - id: plain"),
				assert: func(decoded settingsFile) {
					s.Equal(mo.Some(""), decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey)
				},
			},
		}
		for name, testCase := range testCases {
			s.Run(name, func() {
				testCase.assert(decodeSettingsFile(s.T(), testCase.content))
				_, err := New(writeSettings(s.T(), testCase.content)).Load()
				s.Require().Error(err)
			})
		}
	})
	s.Run("non-empty", func() {
		content := replace(validSettings(""), "providers:", "activeUI: glyph-tui\nproviders:")
		content = replace(content, "    baseURL: https://example.com/v1", "    baseURL: https://example.com/v1\n    apiKey:\n      literal: secret")
		content = replace(content, "          wireFormat: openai-responses\n      - id: plain", "          compatibilityKey: family\n          wireFormat: openai-responses\n      - id: plain")
		decoded := decodeSettingsFile(s.T(), content)
		s.Equal(mo.Some("glyph-tui"), decoded.ActiveUI)
		decodedAPIKey, present := decoded.Providers["compatible"].APIKey.Get()
		s.Require().True(present)
		s.Equal(mo.Some("secret"), decodedAPIKey.Literal)
		s.Equal(mo.Some("family"), decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey)

		loaded, err := New(writeSettings(s.T(), content)).Load()
		s.Require().NoError(err)
		s.Equal(mo.Some("glyph-tui"), loaded.ActiveUI)
		apiKey, present := loaded.Providers["compatible"].APIKey.Get()
		s.Require().True(present)
		s.Equal(mo.Some("secret"), apiKey.Literal)
		s.Equal(mo.Some("family"), loaded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey)
	})
}

// TestLoadPreservesOptionalPricing verifies absent, flat, zero, and tiered model pricing.
func (s *SettingsSuite) TestLoadPreservesOptionalPricing() {
	// Arrange valid model pricing forms and their exact expected values.
	testCases := map[string]struct {
		yaml    string
		pricing mo.Option[model.Pricing]
	}{
		"absent": {yaml: "", pricing: mo.None[model.Pricing]()},
		"flat": {
			yaml: "        pricing:\n          input: 1.25\n          output: 10\n          cacheRead: 0.125\n          cacheWrite: 1.25",
			pricing: mo.Some(model.Pricing{
				Input: 1.25, Output: 10, CacheRead: 0.125, CacheWrite: 1.25, Tiers: nil,
			}),
		},
		"zero": {
			yaml: "        pricing:\n          input: 0\n          output: 0\n          cacheRead: 0\n          cacheWrite: 0",
			pricing: mo.Some(model.Pricing{
				Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Tiers: nil,
			}),
		},
		"tiered": {
			yaml: "        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.1\n          cacheWrite: 0.5\n          tiers:\n            - inputTokensAbove: 100\n              input: 3\n              output: 4\n              cacheRead: 0.3\n              cacheWrite: 0.7\n            - inputTokensAbove: 200\n              input: 5\n              output: 6\n              cacheRead: 0.5\n              cacheWrite: 0.9",
			pricing: mo.Some(model.Pricing{
				Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.5,
				Tiers: []model.PricingTier{
					{InputTokensAbove: 100, Input: 3, Output: 4, CacheRead: 0.3, CacheWrite: 0.7},
					{InputTokensAbove: 200, Input: 5, Output: 6, CacheRead: 0.5, CacheWrite: 0.9},
				},
			}),
		},
	}
	for name, testCase := range testCases {
		s.Run(name, func() {
			content := validSettings("")
			if testCase.yaml != "" {
				content = replace(content, "      - id: compatible", "      - id: compatible\n"+testCase.yaml)
			}

			// Act by loading the settings through the strict decoder and validator.
			settings, err := New(writeSettings(s.T(), content)).Load()

			// Assert pricing presence and configured values are preserved exactly.
			s.Require().NoError(err)
			s.Require().Equal(testCase.pricing, settings.Providers["compatible"].Models[0].Pricing)
		})
	}
}

// TestLoadRejectsInvalidPricing verifies required finite rates and strictly increasing positive thresholds.
func (s *SettingsSuite) TestLoadRejectsInvalidPricing() {
	// Arrange complete flat and tiered mappings used to isolate each invalid field.
	flat := "        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.1\n          cacheWrite: 0.5"
	tiered := flat + "\n          tiers:\n            - inputTokensAbove: 100\n              input: 3\n              output: 4\n              cacheRead: 0.3\n              cacheWrite: 0.7"
	testCases := map[string]string{
		"missing input":            strings.Replace(flat, "          input: 1\n", "", 1),
		"missing output":           strings.Replace(flat, "          output: 2\n", "", 1),
		"missing cache read":       strings.Replace(flat, "          cacheRead: 0.1\n", "", 1),
		"missing cache write":      strings.Replace(flat, "          cacheWrite: 0.5", "", 1),
		"negative input":           strings.Replace(flat, "input: 1", "input: -1", 1),
		"negative output":          strings.Replace(flat, "output: 2", "output: -2", 1),
		"negative cache read":      strings.Replace(flat, "cacheRead: 0.1", "cacheRead: -0.1", 1),
		"negative cache write":     strings.Replace(flat, "cacheWrite: 0.5", "cacheWrite: -0.5", 1),
		"NaN rate":                 strings.Replace(flat, "input: 1", "input: .nan", 1),
		"positive infinity rate":   strings.Replace(flat, "output: 2", "output: .inf", 1),
		"negative infinity rate":   strings.Replace(flat, "cacheRead: 0.1", "cacheRead: -.inf", 1),
		"missing tier threshold":   strings.Replace(tiered, "            - inputTokensAbove: 100\n", "            -\n", 1),
		"missing tier input":       strings.Replace(tiered, "              input: 3\n", "", 1),
		"missing tier output":      strings.Replace(tiered, "              output: 4\n", "", 1),
		"missing tier cache read":  strings.Replace(tiered, "              cacheRead: 0.3\n", "", 1),
		"missing tier cache write": strings.Replace(tiered, "              cacheWrite: 0.7", "", 1),
		"negative tier rate":       strings.Replace(tiered, "input: 3", "input: -3", 1),
		"zero threshold":           strings.Replace(tiered, "inputTokensAbove: 100", "inputTokensAbove: 0", 1),
		"negative threshold":       strings.Replace(tiered, "inputTokensAbove: 100", "inputTokensAbove: -1", 1),
		"duplicate thresholds":     tiered + "\n            - inputTokensAbove: 100\n              input: 5\n              output: 6\n              cacheRead: 0.5\n              cacheWrite: 0.9",
		"unordered thresholds":     strings.Replace(tiered, "inputTokensAbove: 100", "inputTokensAbove: 200", 1) + "\n            - inputTokensAbove: 100\n              input: 5\n              output: 6\n              cacheRead: 0.5\n              cacheWrite: 0.9",
	}
	for name, pricing := range testCases {
		s.Run(name, func() {
			content := replace(validSettings(""), "      - id: compatible", "      - id: compatible\n"+pricing)

			// Act by loading the invalid pricing mapping.
			_, err := New(writeSettings(s.T(), content)).Load()

			// Assert invalid pricing rejects the complete settings file.
			s.Require().Error(err)
		})
	}
}

// TestLoadRejectsUnknownYAMLFields verifies strict decoding at every settings mapping level.
func (s *SettingsSuite) TestLoadRejectsUnknownYAMLFields() {
	// Arrange valid settings with one unknown field at each mapping level.
	testCases := map[string]string{
		"settings":     validSettings("extra: value"),
		"provider":     replace(validSettings(""), "type: openai-compatible", "type: openai-compatible\n    timeout: 1s"),
		"API key":      validSettings("    apiKey:\n      command: echo-key"),
		"model":        replace(validSettings(""), "id: compatible", "id: compatible\n        displayName: Demo"),
		"pricing":      replace(validSettings(""), "id: compatible", "id: compatible\n        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.5\n          cacheWrite: 1\n          currency: USD"),
		"pricing tier": replace(validSettings(""), "id: compatible", "id: compatible\n        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.5\n          cacheWrite: 1\n          tiers:\n            - inputTokensAbove: 100\n              input: 2\n              output: 3\n              cacheRead: 1\n              cacheWrite: 2\n              currency: USD"),
		"reasoning":    replace(validSettings(""), "choices: [off, high]", "choices: [off, high]\n          budget: high"),
	}
	for name, content := range testCases {
		s.Run(name, func() {
			// Act by loading the settings through the strict decoder.
			_, err := New(writeSettings(s.T(), content)).Load()

			// Assert the unknown field rejects the complete settings file.
			s.Require().Error(err)
		})
	}
}

// TestLoadRejectsDuplicateYAMLFields verifies duplicate-key rejection at every settings mapping level.
func (s *SettingsSuite) TestLoadRejectsDuplicateYAMLFields() {
	testCases := map[string]string{
		"settings":  validSettings("defaultProvider: other"),
		"provider":  replace(validSettings(""), "type: openai-compatible", "type: openai-compatible\n    type: openai-compatible"),
		"API key":   validSettings("    apiKey:\n      literal: first\n      literal: second"),
		"model":     replace(validSettings(""), "id: compatible", "id: compatible\n        id: other"),
		"reasoning": replace(validSettings(""), "choices: [off, high]", "choices: [off, high]\n          choices: [off, low]"),
	}
	for name, content := range testCases {
		s.Run(name, func() {
			_, err := New(writeSettings(s.T(), content)).Load()
			s.Require().Error(err)
		})
	}
}

// TestLoadRejectsInvalidReasoning verifies strict capability and wire-format validation.
func (s *SettingsSuite) TestLoadRejectsInvalidReasoning() {
	testCases := map[string]string{
		"duplicate choices":       replace(validSettings(""), "choices: [off, high]", "choices: [off, high, high]"),
		"default outside choices": replace(validSettings(""), "default: high", "default: medium"),
		"contradictory support":   replace(validSettings(""), "supported: true\n          choices: [off, high]", "supported: false\n          choices: [off, high]"),
		"missing support": replace(
			validSettings(""),
			"          supported: false\n          choices: [off]\n          default: off",
			"          choices: [off]\n          default: off",
		),
		"null support": replace(
			validSettings(""),
			"supported: false\n          choices: [off]",
			"supported: null\n          choices: [off]",
		),
		"missing wire format":  withoutLine(validSettings(""), "wireFormat:"),
		"API mismatch":         replace(validSettings(""), "wireFormat: openai-responses", "wireFormat: openai-chat-effort"),
		"unknown wire format":  replace(validSettings(""), "wireFormat: openai-responses", "wireFormat: custom"),
		"Ornith off":           ornithSettings("choices: [off]\n          default: off", "api: chat-completions"),
		"Ornith effort":        ornithSettings("choices: [low]\n          default: low", "api: chat-completions"),
		"Ornith other default": ornithSettings("choices: [off, on]\n          default: off", "api: chat-completions"),
		"Ornith wrong API":     ornithSettings("choices: [on]\n          default: on", "api: responses"),
		"on mixed with effort": replace(validSettings(""), "choices: [off, high]", "choices: [on, high]"),
		"key on non-reasoning": replace(validSettings(""), "supported: false\n          choices: [off]\n          default: off", "supported: false\n          choices: [off]\n          default: off\n          compatibilityKey: shared"),
	}
	for name, content := range testCases {
		s.Run(name, func() {
			_, err := New(writeSettings(s.T(), content)).Load()
			s.Require().Error(err)
		})
	}
}

// TestLoadRejectsInvalidSettings verifies the remaining closed settings rules.
func (s *SettingsSuite) TestLoadRejectsInvalidSettings() {
	testCases := map[string]string{
		"old thinking field":          validSettings("defaultThinkingLevel: high"),
		"missing default provider":    withoutLine(validSettings(""), "defaultProvider:"),
		"missing default model":       withoutLine(validSettings(""), "defaultModel:"),
		"missing providers":           "defaultProvider: openai-codex\ndefaultModel: codex\n",
		"unknown provider":            replace(validSettings(""), "type: openai-compatible", "type: other"),
		"missing codex":               replace(validSettings(""), "openai-codex:", "other-codex:"),
		"second codex":                validSettings("  second-codex:\n    type: openai-codex\n    models:\n      - id: other\n        reasoning:\n          supported: false\n          choices: [off]\n          default: off"),
		"codex wrong identifier":      replace(validSettings(""), "openai-codex:", "codex-provider:"),
		"codex base URL":              replace(validSettings(""), "type: openai-codex", "type: openai-codex\n    baseURL: https://example.com"),
		"codex API":                   replace(validSettings(""), "type: openai-codex", "type: openai-codex\n    api: responses"),
		"codex API key":               replace(validSettings(""), "type: openai-codex", "type: openai-codex\n    apiKey:\n      literal: secret"),
		"codex model API":             replace(validSettings(""), "id: codex", "id: codex\n        api: responses"),
		"missing URL":                 withoutLine(validSettings(""), "baseURL:"),
		"relative URL":                replace(validSettings(""), "https://example.com/v1", "/v1"),
		"non-HTTP URL":                replace(validSettings(""), "https://example.com/v1", "file:///tmp/api"),
		"unknown API":                 replace(validSettings(""), "api: responses", "api: completions"),
		"empty model ID":              replace(validSettings(""), "id: compatible", "id: ''"),
		"duplicate model":             replace(validSettings(""), "      - id: compatible", "      - id: compatible\n        reasoning:\n          supported: false\n          choices: [off]\n          default: off\n      - id: compatible"),
		"unknown model API":           replace(validSettings(""), "id: compatible", "id: compatible\n        api: completions"),
		"unknown default provider":    replace(validSettings(""), "defaultProvider: openai-codex", "defaultProvider: missing"),
		"unknown default model":       replace(validSettings(""), "defaultModel: codex", "defaultModel: missing"),
		"empty API key map":           validSettings("    apiKey: {}"),
		"null API key source":         validSettings("    apiKey:\n      literal: null"),
		"multiple API key fields":     validSettings("    apiKey:\n      environment: API_KEY\n      credential: entry"),
		"empty literal":               validSettings("    apiKey:\n      literal: ''"),
		"empty environment":           validSettings("    apiKey:\n      environment: ''"),
		"empty credential":            validSettings("    apiKey:\n      credential: ''"),
		"empty active UI":             replace(validSettings(""), "providers:", "activeUI: ___---\nproviders:"),
		"multiple YAML documents":     validSettings("") + "---\n" + validSettings(""),
		"provider ID whitespace":      replace(validSettings(""), "compatible:", "' compatible ':"),
		"model ID surrounding spaces": replace(validSettings(""), "id: compatible", "id: ' compatible '"),
	}
	for name, content := range testCases {
		s.Run(name, func() {
			_, err := New(writeSettings(s.T(), content)).Load()
			s.Require().Error(err)
		})
	}
}

// TestLoadRetainsParserCauses verifies settings syntax and URL parser failures keep their original diagnostics.
func (s *SettingsSuite) TestLoadRetainsParserCauses() {
	// Arrange malformed main YAML, malformed trailing YAML, and a URL with an invalid control character.
	testCases := map[string]struct {
		content string
		prefix  string
		cause   string
	}{
		"main YAML": {
			content: "providers: [\n",
			prefix:  "decode Glyph settings:",
			cause:   "did not find expected node content",
		},
		"trailing YAML": {
			content: validSettings("") + "---\nproviders: [\n",
			prefix:  "decode trailing Glyph settings:",
			cause:   "did not find expected node content",
		},
		"base URL": {
			content: replace(validSettings(""), "https://example.com/v1", `"https://example.com/\u0007"`),
			prefix:  "provider \"compatible\":",
			cause:   "net/url: invalid control character in URL",
		},
	}
	for name, test := range testCases {
		s.Run(name, func() {
			// Act by loading the invalid settings through the normal persistence boundary.
			_, err := New(writeSettings(s.T(), test.content)).Load()

			// Assert both field context and the parser's diagnostic remain visible.
			s.Require().Error(err)
			s.Contains(err.Error(), test.prefix)
			s.Contains(err.Error(), test.cause)
		})
	}
}

func decodeSettingsFile(t *testing.T, content string) settingsFile {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(content))
	decoder.KnownFields(true)
	var decoded settingsFile
	require.NoError(t, decoder.Decode(&decoded))
	return decoded
}

func validSettings(extra string) string {
	content := `defaultProvider: openai-codex
defaultModel: codex
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: codex
        reasoning:
          supported: true
          choices: [off, low, high]
          default: high
          wireFormat: openai-responses
  compatible:
    type: openai-compatible
    baseURL: https://example.com/v1
    api: responses
    models:
      - id: compatible
        reasoning:
          supported: true
          choices: [off, high]
          default: high
          wireFormat: openai-responses
      - id: plain
        reasoning:
          supported: false
          choices: [off]
          default: off
`
	if extra == "" {
		return content
	}
	return content + extra + "\n"
}

// ornithSettings builds one Ornith capability and API validation fixture.
func ornithSettings(shape, api string) string {
	content := replace(validSettings(""), "api: responses", api)
	content = replace(content, "choices: [off, high]\n          default: high", shape)
	return replace(content, "wireFormat: openai-responses\n      - id: plain", "wireFormat: ollama-ornith\n      - id: plain")
}

func withoutLine(content, prefix string) string {
	lines := []byte(content)
	for start := 0; start < len(lines); {
		end := start
		for end < len(lines) && lines[end] != '\n' {
			end++
		}
		line := string(lines[start:end])
		trimmed := line
		for len(trimmed) > 0 && trimmed[0] == ' ' {
			trimmed = trimmed[1:]
		}
		if len(trimmed) >= len(prefix) && trimmed[:len(prefix)] == prefix {
			if end < len(lines) {
				end++
			}
			return string(append(lines[:start], lines[end:]...))
		}
		start = end + 1
	}
	return content
}

func replace(content, old, replacement string) string {
	position := index(content, old)
	return content[:position] + replacement + content[position+len(old):]
}

func index(content, target string) int {
	for position := 0; position+len(target) <= len(content); position++ {
		if content[position:position+len(target)] == target {
			return position
		}
	}
	panic("test fixture target not found")
}

// writeSettings stores one settings fixture under a test-owned temporary directory.
func writeSettings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
