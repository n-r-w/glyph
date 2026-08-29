package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.yaml.in/yaml/v3"
)

type SettingsSuite struct{ suite.Suite }

func TestSettingsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SettingsSuite))
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
        input: [text, image]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities:
          strictJSONSchema: false
          grammar:
            lark: false
            regex: false
        reasoning:
          supported: true
          choices: [off, low, high]
          default: high
  compatible:
    type: openai-compatible
    baseURL: https://example.com/v1
    api: responses
    models:
      - id: compatible
        input: [text]
        contextWindow: 65536
        maxTokens: 8192
        toolCapabilities:
          strictJSONSchema: true
          grammar:
            lark: true
            regex: true
        reasoning:
          supported: true
          choices: [off, high]
          default: high
      - id: plain
        input: [text]
        contextWindow: 32768
        maxTokens: 4096
        toolCapabilities: {}
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
