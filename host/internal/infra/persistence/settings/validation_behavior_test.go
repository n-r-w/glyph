//go:build integration

package settings

import (
	"strings"
)

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
		"settings":          validSettings("extra: value"),
		"provider":          replace(validSettings(""), "type: openai-compatible", "type: openai-compatible\n    timeout: 1s"),
		"API key":           validSettings("    apiKey:\n      command: echo-key"),
		"model":             replace(validSettings(""), "id: compatible", "id: compatible\n        displayName: Demo"),
		"pricing":           replace(validSettings(""), "id: compatible", "id: compatible\n        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.5\n          cacheWrite: 1\n          currency: USD"),
		"pricing tier":      replace(validSettings(""), "id: compatible", "id: compatible\n        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.5\n          cacheWrite: 1\n          tiers:\n            - inputTokensAbove: 100\n              input: 2\n              output: 3\n              cacheRead: 1\n              cacheWrite: 2\n              currency: USD"),
		"reasoning":         replace(validSettings(""), "choices: [off, high]", "choices: [off, high]\n          budget: high"),
		"tool capabilities": replace(validSettings(""), "strictJSONSchema: true", "strictJSONSchema: true\n          format: custom"),
		"grammar":           replace(validSettings(""), "lark: true", "lark: true\n            json: true"),
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

// TestLoadRejectsInvalidReasoning verifies provider-neutral capability and structural format validation.
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
		"on mixed with effort":    replace(validSettings(""), "choices: [off, high]", "choices: [on, high]"),
		"key on non-reasoning":    replace(validSettings(""), "supported: false\n          choices: [off]\n          default: off", "supported: false\n          choices: [off]\n          default: off\n          compatibilityKey: shared"),
		"format on non-reasoning": replace(validSettings(""), "supported: false\n          choices: [off]\n          default: off", "supported: false\n          choices: [off]\n          default: off\n          format: provider-private"),
	}
	for name, content := range testCases {
		s.Run(name, func() {
			_, err := New(writeSettings(s.T(), content)).Load()
			s.Require().Error(err)
		})
	}
}

// TestLoadRejectsInvalidModelExecutionCapabilities checks every settings capability invariant and error context.
func (s *SettingsSuite) TestLoadRejectsInvalidModelExecutionCapabilities() {
	testCases := map[string]struct {
		// content is the complete settings document under test.
		content string
		// want is the expected field-specific error.
		want string
	}{
		"empty input": {
			content: replace(validSettings(""), "input: [text, image]", "input: []"),
			want:    `provider "openai-codex" model "codex": input must not be empty`,
		},
		"missing text": {
			content: replace(validSettings(""), "input: [text, image]", "input: [image]"),
			want:    `provider "openai-codex" model "codex": input must contain "text"`,
		},
		"duplicate modality": {
			content: replace(validSettings(""), "input: [text, image]", "input: [text, text]"),
			want:    `provider "openai-codex" model "codex": input contains duplicate modality "text"`,
		},
		"unknown modality": {
			content: replace(validSettings(""), "input: [text, image]", "input: [text, audio]"),
			want:    `provider "openai-codex" model "codex": input contains unknown modality "audio"`,
		},
		"zero context window": {
			content: replace(validSettings(""), "contextWindow: 131072", "contextWindow: 0"),
			want:    `provider "openai-codex" model "codex": contextWindow must be greater than zero`,
		},
		"negative context window": {
			content: replace(validSettings(""), "contextWindow: 131072", "contextWindow: -1"),
			want:    `provider "openai-codex" model "codex": contextWindow must be greater than zero`,
		},
		"zero max tokens": {
			content: replace(validSettings(""), "maxTokens: 16384", "maxTokens: 0"),
			want:    `provider "openai-codex" model "codex": maxTokens must be greater than zero`,
		},
		"negative max tokens": {
			content: replace(validSettings(""), "maxTokens: 16384", "maxTokens: -1"),
			want:    `provider "openai-codex" model "codex": maxTokens must be greater than zero`,
		},
		"max tokens above context window": {
			content: replace(validSettings(""), "maxTokens: 16384", "maxTokens: 131073"),
			want:    `provider "openai-codex" model "codex": maxTokens must not exceed contextWindow`,
		},
		"missing tool capabilities": {
			content: replace(validSettings(""), "        toolCapabilities:\n          strictJSONSchema: false\n          grammar:\n            lark: false\n            regex: false\n", ""),
			want:    `provider "openai-codex" model "codex": toolCapabilities is required`,
		},
	}
	for name, testCase := range testCases {
		s.Run(name, func() {
			// Arrange one settings file with the named invalid capability.
			path := writeSettings(s.T(), testCase.content)

			// Act by loading the settings without cached state.
			_, err := New(path).Load()

			// Assert the error identifies the provider, model, field, and violated rule.
			s.Require().ErrorContains(err, testCase.want)
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
