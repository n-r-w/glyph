//go:build integration

package app

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildGlyphBinary builds the production one-shot executable.
func buildGlyphBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "glyph")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./host/cmd/glyph")
	command.Dir = repoRoot(t)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return binary
}

// oneShotSettings adds a local Responses provider with the configured models and selects its text-only model.
func oneShotSettings(baseURL string) string {
	local := strings.Replace(
		strings.SplitN(configuredRequestSettings, "providers:\n", 2)[1],
		"  openai-codex:\n    type: openai-codex",
		"  local:\n    type: openai-compatible\n    api: responses\n    baseURL: "+baseURL,
		1,
	)
	settings := strings.Replace(configuredRequestSettings, "defaultProvider: openai-codex", "defaultProvider: local", 1)
	return strings.Replace(settings, "providers:\n", "providers:\n"+local, 1)
}

// TestPublicInputMiddlewareOneShot verifies one-shot output, exit status and provider requests of the input chain.
func TestPublicInputMiddlewareOneShot(t *testing.T) {
	t.Parallel()
	// Arrange the production executable once for all scenarios.
	binary := buildGlyphBinary(t)
	for _, test := range []struct {
		// name identifies the scenario.
		name string
		// mode selects the reference executable.
		mode string
		// text is the one-shot request.
		text string
		// failure is the complete visible error line; empty means success.
		failure string
		// requests is the expected number of provider requests.
		requests int
	}{
		{name: "transformed text", mode: "input", text: userInputFollowUpText, failure: "", requests: 1},
		{name: "handled input", mode: "input", text: inputHandledText, failure: "", requests: 0},
		{
			name: "handler failure", mode: "input-fail", text: userInputFollowUpText, requests: 0,
			failure: `[error] EXTENSION_FAILED: run headless agent: process user input: ` +
				`middleware handler "input-text": ` + inputFailureText,
		},
		{
			name: "handler-introduced image", mode: "input-add-image", text: userInputFollowUpText, requests: 0,
			failure: "[error] INPUT_INVALID: run headless agent: process user input: " +
				"model gpt-active does not accept image input at content 1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a local provider endpoint, an isolated home and the selected reference scenario.
			bodies := new(requestBodies)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Error(err)
					return
				}
				bodies.append(body)
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte(finalResponseSSE))
			}))
			t.Cleanup(server.Close)
			home := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(home, ".glyph"), 0o700))
			require.NoError(t, os.WriteFile(
				filepath.Join(home, ".glyph", "settings.yaml"), []byte(oneShotSettings(server.URL)), 0o600,
			))
			directory := buildInputFixture(t, test.mode)

			// Act by running the production executable in one-shot mode.
			command := exec.CommandContext(t.Context(), binary, "run", "--extension-dir", directory, test.text)
			command.Env = append(os.Environ(), "HOME="+home)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			runErr := command.Run()

			// Assert the exit status, the visible category with complete text, and the provider requests.
			received := bodies.snapshot()
			require.Len(t, received, test.requests, stderr.String())
			if test.failure != "" {
				exitErr, exited := errors.AsType[*exec.ExitError](runErr)
				require.True(t, exited, runErr)
				assert.Equal(t, 1, exitErr.ExitCode())
				assert.Contains(t, stderr.String(), test.failure+"\n")
				return
			}
			require.NoError(t, runErr, stderr.String())
			assert.NotContains(t, stderr.String(), "[error]")
			if test.requests == 1 {
				contents := providerUserContent(t, received[0])
				assert.JSONEq(t, transformedFollowUpProvider, contents[len(contents)-1])
			}
		})
	}
}
