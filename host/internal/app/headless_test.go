//go:build integration

package app

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

// TestRunHeadlessUsesCompatibleDefaultWithoutAuthorization verifies the default runtime and keyless request.
func TestRunHeadlessUsesCompatibleDefaultWithoutAuthorization(t *testing.T) {
	t.Parallel()

	requestCount := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		assert.Equal(t, "/v1/chat/completions", request.URL.Path)
		assert.Empty(t, request.Header.Values("Authorization"))
		writer.Header().Set("Content-Type", "text/event-stream")
		_, err := io.WriteString(
			writer,
			"data: {\"id\":\"chat-1\",\"model\":\"local-model\","+
				"\"choices\":[{\"index\":0,\"delta\":{\"content\":\"compatible "+
				"response\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	paths := testPaths(t, fmt.Sprintf(`defaultProvider: local
defaultModel: local-model
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: codex-model
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning:
          supported: false
          choices: [off]
          default: off
  local:
    type: openai-compatible
    baseURL: %s/v1
    api: chat-completions
    models:
      - id: local-model
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning:
          supported: false
          choices: [off]
          default: off
`, server.URL))
	var stdout, stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           "request",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, int32(1), requestCount.Load())
	assert.Equal(t, "compatible response\n", stdout.String())
}

// TestRunWithPathsUIInvalidSettingsStopsBeforeLogging verifies capability validation precedes UI startup effects.
func TestRunWithPathsUIInvalidSettingsStopsBeforeLogging(t *testing.T) {
	t.Parallel()

	// Arrange invalid model input and a UI process marker.
	paths := testPaths(t, strings.Replace(codexSettings(""), "input: [text]", "input: [image]", 1))
	uiDirectory := t.TempDir()
	startupMarker := filepath.Join(t.TempDir(), "ui-started")
	writeConfiguredUIExecutable(t, uiDirectory, "Invalid_UI", startupMarker, "snapshot")

	// Act by starting UI mode with invalid model capabilities.
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "invalid-ui",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert the field-specific error arrives before UI startup and logging.
	require.ErrorContains(t, err, `provider "openai-codex" model "gpt-test": input must contain "text"`)
	_, markerErr := os.Stat(startupMarker)
	require.ErrorIs(t, markerErr, os.ErrNotExist)
	_, logErr := os.Stat(paths.LogFile)
	require.ErrorIs(t, logErr, os.ErrNotExist)
}

// TestRunWithPathsHeadlessInvalidSettingsStopsBeforeRun verifies capability validation precedes provider dispatch.
//
//nolint:paralleltest // The test replaces process-global HTTP transport to observe provider dispatch.
func TestRunWithPathsHeadlessInvalidSettingsStopsBeforeRun(t *testing.T) {
	// Arrange invalid model input, a provider transport counter, and run output buffers.
	paths := testPaths(t, strings.Replace(codexSettings(""), "input: [text]", "input: [image]", 1))
	requests := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = countingFailureTransport{requests: requests}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Act by starting headless mode with invalid model capabilities.
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           "request",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
	}, &stdout, &stderr)

	// Assert the field-specific error arrives before provider dispatch or run output.
	require.ErrorContains(t, err, `provider "openai-codex" model "gpt-test": input must contain "text"`)
	assert.Zero(t, requests.Load())
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
}
