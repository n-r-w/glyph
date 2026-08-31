package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"
)

// restartSelectionSettings returns the local provider fixture used by restart tests.
func restartSelectionSettings() string {
	return `defaultProvider: local
defaultModel: local-model
providers:
  local:
    type: openai-compatible
    baseURL: http://localhost:11434/v1
    api: chat-completions
    models:
      - id: local-model
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing:
          input: 0
          output: 0
          cacheRead: 0
          cacheWrite: 0
        reasoning:
          supported: false
          choices: [off]
          default: off
  openai-codex:
    type: openai-codex
    models:
      - id: selected-model
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing:
          input: 0
          output: 0
          cacheRead: 0
          cacheWrite: 0
        reasoning:
          supported: true
          choices: [low, high]
          default: low
`
}

// pricedRestartSelectionSettings applies nonzero selected-model rates for cost reconstruction tests.
func pricedRestartSelectionSettings() string {
	return strings.Replace(
		restartSelectionSettings(),
		"      - id: selected-model\n        input: [text]\n        "+
			"contextWindow: 131072\n        maxTokens: 16384\n        "+
			"toolCapabilities: {}\n        pricing:\n          input: 0\n     "+
			"     output: 0\n          cacheRead: 0\n          cacheWrite: 0",
		"      - id: selected-model\n        input: [text]\n        "+
			"contextWindow: 131072\n        maxTokens: 16384\n        "+
			"toolCapabilities: {}\n        pricing:\n          input: 1\n     "+
			"     output: 2\n          cacheRead: 3\n          cacheWrite: 4",
		1,
	)
}

// pricedCodexSettings uses distinct rates so every mapped cost bucket is observable.
func pricedCodexSettings() string {
	return strings.Replace(
		codexSettings(""),
		"          input: 0\n          output: 0\n          cacheRead: 0\n          cacheWrite: 0",
		"          input: 1\n          output: 2\n          cacheRead: 3\n          cacheWrite: 4",
		1,
	)
}

// codexSettings returns the strict default Codex fixture used by application tests.
func codexSettings(extra string) string {
	return `defaultProvider: openai-codex
defaultModel: gpt-test
` + extra + `providers:
  openai-codex:
    type: openai-codex
    models:
      - id: gpt-test
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing:
          input: 0
          output: 0
          cacheRead: 0
          cacheWrite: 0
        reasoning:
          supported: false
          choices: [off]
          default: off
`
}

// testPaths creates one owner-only Glyph data directory and strict settings fixture.
func testPaths(t *testing.T, settingsContent string) persistence.Paths {
	t.Helper()
	directory := filepath.Join(t.TempDir(), ".glyph")
	require.NoError(t, os.Mkdir(directory, 0o700))
	settingsPath := filepath.Join(directory, "settings.yaml")
	require.NoError(t, os.WriteFile(settingsPath, []byte(settingsContent), 0o600))
	logsDirectory := filepath.Join(directory, "logs")
	return persistence.Paths{
		Directory:       directory,
		SettingsFile:    settingsPath,
		CredentialsFile: filepath.Join(directory, "credentials.json"),
		LogsDirectory:   logsDirectory,
		LogFile:         filepath.Join(logsDirectory, "glyph.log"),
	}
}

// validateBusyPreservation verifies that a rejected resume leaves all active UI state visible.
func validateBusyPreservation(output, activeID string) error {
	if !strings.Contains(output, "Session status: Session replacement is unavailable: another operation is active") {
		return errors.New("busy redraw did not occur after the rejection")
	}
	required := []struct {
		text    string
		message string
	}{
		{text: "user: blocked request", message: "busy screen did not preserve the active user text"},
		{text: "Session ID: " + activeID, message: "busy screen did not preserve the active session ID"},
		{text: "Name: <absent>", message: "busy screen did not preserve the active session name state"},
		{text: "/resume|", message: "busy screen did not preserve the /resume editor draft"},
		{text: "Sessions:", message: "busy screen did not preserve the session selector"},
		{text: "Selector: Up/Down navigate", message: "busy screen did not preserve the open selector"},
	}
	for _, item := range required {
		if !strings.Contains(output, item.text) {
			return errors.New(item.message)
		}
	}
	if err := validateRestartRow(output); err != nil {
		return errors.New("busy screen did not preserve the restart session row")
	}
	if !strings.Contains(output, "> restart session") {
		return errors.New("busy screen did not preserve the exact selected restart session row")
	}
	return nil
}

// validateRestartRow verifies that the selector preserves the expected resumed-session history count.
func validateRestartRow(output string) error {
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "restart session") && strings.Contains(line, "7 messages") {
			return nil
		}
	}
	return errors.New("restart selector did not show restart session with 7 messages")
}

type tokenUsageObservation struct {
	Present          bool  `json:"present"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// costObservation records optional cost presence and all five public fields.
type costObservation struct {
	Present    bool    `json:"present"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Total      float64 `json:"total"`
}

type statisticsObservation struct {
	UserMessages   int64                 `json:"user_messages"`
	ModelResponses int64                 `json:"model_responses"`
	ToolCalls      int64                 `json:"tool_calls"`
	ToolResults    int64                 `json:"tool_results"`
	TotalMessages  int64                 `json:"total_messages"`
	Tokens         tokenUsageObservation `json:"tokens"`
	EstimatedCost  costObservation       `json:"estimated_cost"`
	CostGroupCount int                   `json:"cost_group_count"`
	GroupProvider  string                `json:"group_provider"`
	GroupModel     string                `json:"group_model"`
	GroupCost      costObservation       `json:"group_cost"`
}
