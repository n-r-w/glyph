package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
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
	s.Equal("glyph-tui-plugin", loaded.ActiveUI)
	models := loaded.Providers["openrouter"].Models
	s.Require().Len(models, 6)
	s.Equal(Reasoning{
		Supported: true, Choices: []ReasoningChoice{ReasoningChoiceMinimal, ReasoningChoiceMedium, ReasoningChoiceHigh},
		Default: ReasoningChoiceMedium, CompatibilityKey: "", WireFormat: ReasoningWireFormatOpenAIResponses,
	}, models[0].Reasoning)
	s.Equal([]ReasoningChoice{ReasoningChoiceOff, ReasoningChoiceOn}, models[1].Reasoning.Choices)
	s.Equal(ReasoningChoiceOn, models[2].Reasoning.Default)
	s.Equal(Reasoning{
		Supported:        true,
		Choices:          []ReasoningChoice{ReasoningChoiceOff, ReasoningChoiceLow, ReasoningChoiceHigh},
		Default:          ReasoningChoiceLow,
		CompatibilityKey: "",
		WireFormat:       ReasoningWireFormatOpenAIChatEffort,
	}, models[3].Reasoning)
	s.Equal(Reasoning{
		Supported: true, Choices: []ReasoningChoice{ReasoningChoiceOn}, Default: ReasoningChoiceOn,
		CompatibilityKey: "", WireFormat: ReasoningWireFormatOllamaOrnith,
	}, models[4].Reasoning)
	s.Equal(Reasoning{
		Supported: false, Choices: []ReasoningChoice{ReasoningChoiceOff}, Default: ReasoningChoiceOff,
		CompatibilityKey: "", WireFormat: "",
	}, models[5].Reasoning)
}

// TestLoadAcceptsEachAPIKeySource verifies the structured union's three valid variants.
func (s *SettingsSuite) TestLoadAcceptsEachAPIKeySource() {
	for name, source := range map[string]string{
		"literal": "literal: '!not-a-command'", "environment": "environment: GLYPH_TEST_API_KEY", "credential": "credential: local-entry",
	} {
		s.Run(name, func() {
			loaded, err := New(writeSettings(s.T(), validSettings("    apiKey:\n      "+source))).Load()
			s.Require().NoError(err)
			s.NotNil(loaded.Providers["compatible"].APIKey)
		})
	}
}

// TestLoadRejectsInvalidReasoning verifies strict capability and wire-format validation.
func (s *SettingsSuite) TestLoadRejectsInvalidReasoning() {
	testCases := map[string]string{
		"duplicate choices":       replace(validSettings(""), "choices: [off, high]", "choices: [off, high, high]"),
		"default outside choices": replace(validSettings(""), "default: high", "default: medium"),
		"contradictory support":   replace(validSettings(""), "supported: true\n          choices: [off, high]", "supported: false\n          choices: [off, high]"),
		"missing wire format":     withoutLine(validSettings(""), "wireFormat:"),
		"API mismatch":            replace(validSettings(""), "wireFormat: openai-responses", "wireFormat: openai-chat-effort"),
		"unknown wire format":     replace(validSettings(""), "wireFormat: openai-responses", "wireFormat: custom"),
		"Ornith off":              ornithSettings("choices: [off]\n          default: off", "api: chat-completions"),
		"Ornith effort":           ornithSettings("choices: [low]\n          default: low", "api: chat-completions"),
		"Ornith other default":    ornithSettings("choices: [off, on]\n          default: off", "api: chat-completions"),
		"Ornith wrong API":        ornithSettings("choices: [on]\n          default: on", "api: responses"),
		"on mixed with effort":    replace(validSettings(""), "choices: [off, high]", "choices: [on, high]"),
		"key on non-reasoning":    replace(validSettings(""), "supported: false\n          choices: [off]\n          default: off", "supported: false\n          choices: [off]\n          default: off\n          compatibilityKey: shared"),
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
		"unknown root field":          validSettings("extra: value"),
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
		"provider unknown field":      replace(validSettings(""), "type: openai-compatible", "type: openai-compatible\n    timeout: 1s"),
		"empty model ID":              replace(validSettings(""), "id: compatible", "id: ''"),
		"duplicate model":             replace(validSettings(""), "      - id: compatible", "      - id: compatible\n        reasoning:\n          supported: false\n          choices: [off]\n          default: off\n      - id: compatible"),
		"unknown model API":           replace(validSettings(""), "id: compatible", "id: compatible\n        api: completions"),
		"model unknown field":         replace(validSettings(""), "id: compatible", "id: compatible\n        displayName: Demo"),
		"unknown default provider":    replace(validSettings(""), "defaultProvider: openai-codex", "defaultProvider: missing"),
		"unknown default model":       replace(validSettings(""), "defaultModel: codex", "defaultModel: missing"),
		"empty API key map":           validSettings("    apiKey: {}"),
		"multiple API key fields":     validSettings("    apiKey:\n      environment: API_KEY\n      credential: entry"),
		"empty literal":               validSettings("    apiKey:\n      literal: ''"),
		"empty environment":           validSettings("    apiKey:\n      environment: ''"),
		"empty credential":            validSettings("    apiKey:\n      credential: ''"),
		"unknown API key field":       validSettings("    apiKey:\n      command: echo-key"),
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

// TestLoadDecodeErrorsDoNotExposeLiteral verifies initial and trailing YAML failures are secret-free.
func (s *SettingsSuite) TestLoadDecodeErrorsDoNotExposeLiteral() {
	const secret = "s3cr3t"
	for _, content := range []string{
		validSettings("    apiKey: " + secret),
		validSettings("") + "---\nvalue: *" + secret + "\n",
	} {
		_, err := New(writeSettings(s.T(), content)).Load()
		s.Require().Error(err)
		s.NotContains(err.Error(), secret)
	}
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
