package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceLoadParsesStrictSettings verifies every approved field and active UI normalization.
func TestServiceLoadParsesStrictSettings(t *testing.T) {
	t.Parallel()

	path := writeSettings(t, `defaultProvider: openai-codex
defaultModel: gpt-5.6-luna
defaultThinkingLevel: high
activeUI: "  Glyph__TUI--Plugin  "
`)

	loaded, err := New(path).Load()

	require.NoError(t, err)
	require.NotNil(t, loaded.DefaultThinkingLevel)
	assert.Equal(t, ThinkingLevelHigh, *loaded.DefaultThinkingLevel)
	assert.Equal(t, "openai-codex", loaded.DefaultProvider)
	assert.Equal(t, "gpt-5.6-luna", loaded.DefaultModel)
	assert.Equal(t, "glyph-tui-plugin", loaded.ActiveUI)
}

// TestServiceLoadOmitsOptionalSettings verifies model defaults remain distinguishable from configured values.
func TestServiceLoadOmitsOptionalSettings(t *testing.T) {
	t.Parallel()

	path := writeSettings(t, "defaultProvider: openai-codex\ndefaultModel: gpt-5.6-luna\n")

	loaded, err := New(path).Load()

	require.NoError(t, err)
	assert.Nil(t, loaded.DefaultThinkingLevel)
	assert.Empty(t, loaded.ActiveUI)
}

// TestServiceLoadAcceptsSDKThinkingLevels verifies the complete approved SDK enum.
func TestServiceLoadAcceptsSDKThinkingLevels(t *testing.T) {
	t.Parallel()

	levels := []ThinkingLevel{
		ThinkingLevelNone,
		ThinkingLevelMinimal,
		ThinkingLevelLow,
		ThinkingLevelMedium,
		ThinkingLevelHigh,
		ThinkingLevelXHigh,
		ThinkingLevelMax,
	}
	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			path := writeSettings(t, "defaultProvider: openai-codex\ndefaultModel: model\ndefaultThinkingLevel: "+string(level)+"\n")

			loaded, err := New(path).Load()

			require.NoError(t, err)
			require.NotNil(t, loaded.DefaultThinkingLevel)
			assert.Equal(t, level, *loaded.DefaultThinkingLevel)
		})
	}
}

// TestServiceLoadRejectsInvalidSettings verifies strict parsing and closed validation rules.
func TestServiceLoadRejectsInvalidSettings(t *testing.T) {
	t.Parallel()

	testCases := map[string]string{
		"unknown field":        "defaultProvider: openai-codex\ndefaultModel: model\nextra: value\n",
		"missing provider":     "defaultModel: model\n",
		"missing model":        "defaultProvider: openai-codex\n",
		"unsupported provider": "defaultProvider: other\ndefaultModel: model\n",
		"unsupported thinking": "defaultProvider: openai-codex\ndefaultModel: model\ndefaultThinkingLevel: extreme\n",
		"empty active UI":      "defaultProvider: openai-codex\ndefaultModel: model\nactiveUI: ___---\n",
		"multiple documents":   "defaultProvider: openai-codex\ndefaultModel: model\n---\ndefaultProvider: openai-codex\ndefaultModel: other\n",
	}
	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(writeSettings(t, content)).Load()

			require.Error(t, err)
		})
	}
}

// writeSettings stores one settings fixture under a test-owned temporary directory.
func writeSettings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
