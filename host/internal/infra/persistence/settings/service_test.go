package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type SettingsSuite struct {
	suite.Suite
}

func TestSettingsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SettingsSuite))
}

// TestLoadParsesProviderMap verifies the complete settings contract and configured model order.
func (s *SettingsSuite) TestLoadParsesProviderMap() {
	path := writeSettings(s.T(), `defaultProvider: openrouter
defaultModel: openai/gpt-5
defaultReasoningLevel: high
activeUI: "  Glyph__TUI--Plugin  "
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: gpt-5.6-luna
        reasoningLevels: [none, low, high]
  openrouter:
    type: openai-compatible
    baseURL: https://openrouter.ai/api/v1
    api: chat-completions
    apiKey:
      environment: OPENROUTER_API_KEY
    models:
      - id: anthropic/claude-sonnet-4
        reasoningLevels: [none, medium]
      - id: openai/gpt-5
        api: responses
        reasoningLevels: [low, high]
  ollama:
    type: openai-compatible
    baseURL: http://localhost:11434/v1
    api: chat-completions
    models:
      - id: qwen3-coder
        reasoningLevels: [none]
`)

	loaded, err := New(path).Load()

	s.Require().NoError(err)
	s.Equal("openrouter", loaded.DefaultProvider)
	s.Equal("openai/gpt-5", loaded.DefaultModel)
	s.Equal(ReasoningLevelHigh, loaded.DefaultReasoningLevel)
	s.Equal("glyph-tui-plugin", loaded.ActiveUI)
	s.Require().Len(loaded.Providers, 3)
	openrouter := loaded.Providers["openrouter"]
	s.Equal(ProviderTypeOpenAICompatible, openrouter.Type)
	s.Equal("https://openrouter.ai/api/v1", openrouter.BaseURL)
	s.Equal(APIChatCompletions, openrouter.API)
	s.Require().NotNil(openrouter.APIKey)
	s.Require().NotNil(openrouter.APIKey.Environment)
	s.Equal("OPENROUTER_API_KEY", *openrouter.APIKey.Environment)
	s.Require().Len(openrouter.Models, 2)
	s.Equal("anthropic/claude-sonnet-4", openrouter.Models[0].ID)
	s.Equal("openai/gpt-5", openrouter.Models[1].ID)
	s.Equal(APIResponses, openrouter.Models[1].API)
	s.Equal([]ReasoningLevel{ReasoningLevelLow, ReasoningLevelHigh}, openrouter.Models[1].ReasoningLevels)
	s.Nil(loaded.Providers["ollama"].APIKey)
}

// TestLoadAcceptsEachAPIKeySource verifies the structured union's three valid variants.
func (s *SettingsSuite) TestLoadAcceptsEachAPIKeySource() {
	testCases := map[string]string{
		"literal":     "literal: '!not-a-command'",
		"environment": "environment: GLYPH_TEST_API_KEY",
		"credential":  "credential: local-entry",
	}
	for name, source := range testCases {
		s.Run(name, func() {
			content := validSettings("    apiKey:\n      " + source)
			loaded, err := New(writeSettings(s.T(), content)).Load()
			s.Require().NoError(err)
			s.NotNil(loaded.Providers["compatible"].APIKey)
		})
	}
}

// TestLoadRejectsInvalidSettings verifies strict parsing and all closed validation rules.
func (s *SettingsSuite) TestLoadRejectsInvalidSettings() {
	testCases := map[string]string{
		"unknown root field":          validSettings("extra: value"),
		"old thinking field":          validSettings("defaultThinkingLevel: high"),
		"missing default provider":    withoutLine(validSettings(""), "defaultProvider:"),
		"missing default model":       withoutLine(validSettings(""), "defaultModel:"),
		"missing default reasoning":   withoutLine(validSettings(""), "defaultReasoningLevel:"),
		"missing providers":           "defaultProvider: openai-codex\ndefaultModel: codex-model\ndefaultReasoningLevel: none\n",
		"unknown provider type":       replace(validSettings(""), "type: openai-compatible", "type: other"),
		"missing codex":               replace(validSettings(""), "openai-codex:", "other-codex:"),
		"second codex":                validSettings("  second-codex:\n    type: openai-codex\n    models:\n      - id: other\n        reasoningLevels: [none]"),
		"codex wrong identifier":      replace(validSettings(""), "openai-codex:", "codex:"),
		"codex base URL":              replace(validSettings(""), "type: openai-codex", "type: openai-codex\n    baseURL: https://example.com"),
		"codex API":                   replace(validSettings(""), "type: openai-codex", "type: openai-codex\n    api: responses"),
		"codex API key":               replace(validSettings(""), "type: openai-codex", "type: openai-codex\n    apiKey:\n      literal: secret"),
		"codex model API":             replace(validSettings(""), "id: codex-model", "id: codex-model\n        api: responses"),
		"compatible missing URL":      withoutLine(validSettings(""), "baseURL:"),
		"compatible relative URL":     replace(validSettings(""), "https://example.com/v1", "/v1"),
		"compatible non-HTTP URL":     replace(validSettings(""), "https://example.com/v1", "file:///tmp/api"),
		"compatible unknown API":      replace(validSettings(""), "api: chat-completions", "api: completions"),
		"provider unknown field":      replace(validSettings(""), "type: openai-compatible", "type: openai-compatible\n    timeout: 1s"),
		"empty model list":            replace(validSettings(""), "models:\n      - id: compatible-model\n        reasoningLevels: [none, high]", "models: []"),
		"empty model ID":              replace(validSettings(""), "id: compatible-model", "id: ''"),
		"duplicate model ID":          replace(validSettings(""), "reasoningLevels: [none, high]", "reasoningLevels: [none, high]\n      - id: compatible-model\n        reasoningLevels: [none]"),
		"empty reasoning levels":      replace(validSettings(""), "reasoningLevels: [none, high]", "reasoningLevels: []"),
		"duplicate reasoning level":   replace(validSettings(""), "reasoningLevels: [none, high]", "reasoningLevels: [none, high, none]"),
		"unknown reasoning level":     replace(validSettings(""), "reasoningLevels: [none, high]", "reasoningLevels: [none, extreme]"),
		"unknown model API":           replace(validSettings(""), "id: compatible-model", "id: compatible-model\n        api: completions"),
		"model unknown field":         replace(validSettings(""), "id: compatible-model", "id: compatible-model\n        displayName: Demo"),
		"unknown default provider":    replace(validSettings(""), "defaultProvider: openai-codex", "defaultProvider: missing"),
		"unknown default model":       replace(validSettings(""), "defaultModel: codex-model", "defaultModel: missing"),
		"unsupported default level":   replace(validSettings(""), "defaultReasoningLevel: none", "defaultReasoningLevel: high"),
		"empty API key map":           validSettings("    apiKey: {}"),
		"multiple API key fields":     validSettings("    apiKey:\n      environment: API_KEY\n      credential: entry"),
		"empty literal":               validSettings("    apiKey:\n      literal: ''"),
		"empty environment":           validSettings("    apiKey:\n      environment: ''"),
		"empty credential":            validSettings("    apiKey:\n      credential: ''"),
		"unknown API key field":       validSettings("    apiKey:\n      command: echo-key"),
		"empty active UI":             replace(validSettings(""), "providers:", "activeUI: ___---\nproviders:"),
		"multiple YAML documents":     validSettings("") + "---\n" + validSettings(""),
		"provider ID whitespace":      replace(validSettings(""), "compatible:", "' compatible ':"),
		"model ID surrounding spaces": replace(validSettings(""), "id: compatible-model", "id: ' compatible-model '"),
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
	contents := []string{
		validSettings("    apiKey: " + secret),
		validSettings("") + "---\nvalue: *" + secret + "\n",
	}
	for _, content := range contents {
		_, err := New(writeSettings(s.T(), content)).Load()
		s.Require().Error(err)
		s.NotContains(err.Error(), secret)
	}
}

func validSettings(extra string) string {
	content := `defaultProvider: openai-codex
defaultModel: codex-model
defaultReasoningLevel: none
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: codex-model
        reasoningLevels: [none]
  compatible:
    type: openai-compatible
    baseURL: https://example.com/v1
    api: chat-completions
    models:
      - id: compatible-model
        reasoningLevels: [none, high]
`
	if extra == "" {
		return content
	}
	return content + extra + "\n"
}

func withoutLine(content, prefix string) string {
	lines := []byte(content)
	start := 0
	for start < len(lines) {
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
	return string([]byte(content[:index(content, old)])) + replacement + content[index(content, old)+len(old):]
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
