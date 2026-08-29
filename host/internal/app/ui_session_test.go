package app

import (
	"bytes"

	"encoding/json/v2"

	"fmt"

	"net/http"

	"os"

	"path/filepath"

	"sync/atomic"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

// TestRunWithPathsUISessionLifecycleSurvivesRestart verifies Host UI restart restores full public and continuation content.
func TestRunWithPathsUISessionLifecycleSurvivesRestart(t *testing.T) {
	// Arrange persistent paths, credentials, provider transport, UI helper, and extension tools.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(
		nil,
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	), 0o600))
	requestCount := new(atomic.Int32)
	requestCount.Store(0)
	lastBody := new(atomic.Value)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = deterministicCodexTransport{requestCount: requestCount, lastBody: lastBody}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Session_Restart_UI")
	extensionDirectory := buildToolsExecutable(t)
	tracePath := filepath.Join(t.TempDir(), "session-restart.json")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "session-restart")
	command := cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText: "", ExtensionDirectory: extensionDirectory,
		},
		ExtensionDirectory: extensionDirectory, UIDirectory: uiDirectory, UIID: "session-restart-ui", SocketPath: "",
	}

	// Act by running once, appending full content, restarting, and explicitly resuming the named session.
	require.NoError(t, runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{}))
	partialPayload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	var partial sessionRestartObservation
	require.NoError(t, json.Unmarshal(partialPayload, &partial))
	require.False(t, partial.Complete)
	appendFullContentFixture(t, paths, partial.NamedSession.ID)
	require.NoError(t, runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{}))
	payload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	var observation sessionRestartObservation
	require.NoError(t, json.Unmarshal(payload, &observation))
	// Assert public restoration, provider continuation, selection, ordering, and private-data exclusion.
	require.True(t, observation.Complete)
	require.NotEqual(t, observation.NamedSession.ID, observation.NewStartup.ID)
	require.Equal(t, observation.NamedSession.WorkingDirectory, observation.NewStartup.WorkingDirectory)
	require.True(t, observation.NewStartup.IDPresent)
	require.True(t, observation.NewStartup.WorkingDirectoryPresent)
	require.True(t, observation.NewStartup.CreatedTimePresent)
	require.True(t, observation.NewStartup.UpdateTimePresent)
	require.False(t, observation.NewStartup.NamePresent)
	require.False(t, observation.NewStartup.StoragePathPresent)
	require.Equal(t, int32(3), requestCount.Load())
	body, ok := lastBody.Load().([]byte)
	require.True(t, ok)
	require.Contains(t, string(body), `"model":"selected-model"`)
	require.Contains(t, string(body), `"effort":"high"`)
	require.Contains(t, string(body), "restart text")
	require.Contains(t, string(body), "enc-restart")
	require.Contains(t, string(body), "call-1")
	require.Contains(t, string(body), "tool-ok")
	require.Contains(t, string(body), "Request complete.")
	require.Contains(t, string(body), "full user")
	require.Contains(t, string(body), fullContentUserImageBase64)
	require.Contains(t, string(body), "enc-full")
	require.Contains(t, string(body), "full refusal")
	require.Contains(t, string(body), "full-call")
	require.Contains(t, string(body), "full tool output")
	require.Contains(t, string(body), fullContentToolImageBase64)
	require.NotContains(t, string(body), "full-extension")
	require.Contains(t, string(body), "continue")
	require.Less(t, bytes.Index(body, []byte("restart text")), bytes.Index(body, []byte(`"type":"function_call"`)))
	require.Less(t, bytes.Index(body, []byte(`"type":"function_call"`)), bytes.Index(body, []byte(`"type":"function_call_output"`)))
	require.Less(t, bytes.Index(body, []byte(`"type":"function_call_output"`)), bytes.Index(body, []byte("Request complete.")))
	require.Less(t, bytes.Index(body, []byte("Request complete.")), bytes.Index(body, []byte("continue")))
}

// TestRunWithPathsUISessionRecoveryPaths verifies the UI process rejects corruption and recovers one interrupted tail.
func TestRunWithPathsUISessionRecoveryPaths(t *testing.T) {
	// Arrange a two-run Host UI helper and one persisted startup session.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(
		nil,
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	), 0o600))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Session_Recovery_UI")
	tracePath := filepath.Join(t.TempDir(), "session-recovery.json")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "session-recovery")
	command := cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText: "", ExtensionDirectory: "",
		},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "session-recovery-ui", SocketPath: "",
	}
	require.NoError(t, runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{}))
	partialPayload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	var partial sessionRestartObservation
	require.NoError(t, json.Unmarshal(partialPayload, &partial))
	fixtures := writeSessionRecoveryFixture(t, partial.NamedSession.StoragePath, partial.NamedSession.WorkingDirectory)

	// Act by starting a new Host UI process that exercises invalid and interrupted resume paths.
	runErr := runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert the helper observed preserved identity and recovered only the complete entry prefix.
	require.NoError(t, runErr)
	payload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	var observation sessionRestartObservation
	require.NoError(t, json.Unmarshal(payload, &observation))
	assert.True(t, observation.Complete)
	assert.NotEqual(t, observation.NamedSession.ID, observation.NewStartup.ID)
	recovered, err := os.ReadFile(fixtures.interruptedPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasSuffix(recovered, []byte{'\n'}))
	info, err := os.Stat(fixtures.interruptedPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
