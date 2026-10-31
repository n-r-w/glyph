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

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicFinalizedMessagesUI verifies finalized user and model values and a finalization failure through the
// public UI SDK.
func TestPublicFinalizedMessagesUI(t *testing.T) {
	finalText := finalizedFinalText + finalizedModelSuffix
	for _, test := range []struct {
		// name identifies the scenario.
		name string
		// mode selects the reference executable.
		mode string
		// code is the failure category of the first request; empty means completion.
		code string
		// models lists the persisted model texts.
		models []string
		// messageEnds lists the message-end texts of the first request.
		messageEnds []string
	}{
		{
			name: "replacement", mode: "finalized", code: "", models: []string{finalText, finalText},
			messageEnds: []string{finalText},
		},
		{
			name: "invalid action", mode: "finalized-invalid", code: extensionFailedCode, models: []string{finalText},
			messageEnds: []string{},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange the real UI and reference extension processes on an image-capable model.
			t.Setenv(userInputFirstEnvironment, "")

			// Act by submitting text and image input, a text follow-up, and a tree query through the UI SDK.
			observation, bodies := runUIUserInput(t, userInputSettings(true), buildFinalizedFixture(t, test.mode))

			// Assert the first result, persisted values, completion events, and provider requests.
			assert.Equal(t, test.code, observation.FirstCode, observation.FirstError)
			assert.True(t, observation.FollowUpCompleted)
			assert.Equal(t, test.models, observation.Models)
			assert.Equal(t, test.messageEnds, observation.FirstMessageEnds)
			require.Len(t, observation.Users, 2)
			assert.Equal(t, []userInputBlock{
				{Text: userInputFirstText + finalizedUserSuffix, MediaType: "", Data: nil},
				{Text: "", MediaType: userInputMediaType, Data: userInputImageData},
				{Text: userInputLastText + finalizedUserSuffix, MediaType: "", Data: nil},
			}, observation.Users[0])
			assert.Equal(t, []userInputBlock{
				{Text: userInputFollowUpText + finalizedUserSuffix, MediaType: "", Data: nil},
			}, observation.Users[1])
			received := bodies.snapshot()
			require.Len(t, received, 2)
			assert.Contains(t, string(received[1]), userInputFirstText+finalizedUserSuffix)
			assert.Equal(t, test.code == "", strings.Contains(string(received[1]), finalText))
		})
	}
}

// TestPublicFinalizedProviderFailureUI verifies through the public UI SDK that a failed finalization of a retained
// failed response determines the extension-failure category and keeps both complete causes.
func TestPublicFinalizedProviderFailureUI(t *testing.T) {
	// Arrange a provider that rejects the first request and a reference that fails the first model response.
	t.Setenv(userInputFirstEnvironment, "")
	trace := enableFinalizedTrace(t)
	bodies := installStandardOverflowTransport(t, rejectFirstProviderRequest)

	// Act by submitting the failing request, a text follow-up, and a tree query through the UI SDK.
	observation := runUIUserInputFixture(
		t, userInputSettings(true), buildFinalizedFixture(t, "finalized-fail-model"),
	)

	// Assert the category, both causes, no recorded failed response, and one finalization of it.
	assert.Equal(t, extensionFailedCode, observation.FirstCode)
	assert.Contains(t, observation.FirstError, finalizedProviderFailure)
	assert.Contains(t, observation.FirstError, `middleware handler "finalized-mark": `+finalizedFailureText)
	assert.True(t, observation.FollowUpCompleted)
	assert.Equal(t, []string{finalizedFinalText + finalizedModelSuffix}, observation.Models)
	assert.Len(t, bodies.snapshot(), 2)
	handlers := contextTraceHandlers(readContextTrace(t, trace))
	require.GreaterOrEqual(t, len(handlers), 3)
	assert.Equal(t, append(finalizedHandlers(1), finalizedMark), handlers[:3])
	assert.Equal(t, finalizedHandlers(2), handlers[3:])
}

// TestPublicFinalizedConfiguredResult verifies that a configured-model result reaches its extension caller in its
// final form.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport.
func TestPublicFinalizedConfiguredResult(t *testing.T) {
	// Arrange a tool that awaits one configured request and the finalized-message reference.
	directory := buildPublicExtensionFixture(t)
	binary, err := os.ReadFile(filepath.Join(buildFinalizedFixture(t, "finalized"), "finalized"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "finalized"), binary, 0o700))
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := configuredRequestProviderTransport(t, "configured-request", false)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)

	// Act through the outer request whose tool performs the configured request.
	completeProgrammaticRequest(t, fixture, userRequest("configured-finalized", "request configured model"))
	history := finalizedHistory(t, fixture, "configured-history")

	// Assert the tool received the final configured result and the configured response was not persisted.
	require.Len(t, bodies.snapshot(), 3)
	var toolResults []string
	for _, entry := range history {
		if text, found := strings.CutPrefix(entry, "tool: "); found {
			toolResults = append(toolResults, text)
		}
	}
	require.Len(t, toolResults, 1)
	assert.Contains(t, toolResults[0], configuredResponseText+finalizedModelSuffix)
	assert.True(t, strings.HasSuffix(toolResults[0], finalizedToolSuffix), toolResults[0])
	for _, entry := range history {
		assert.NotEqual(t, "model: "+configuredResponseText+finalizedModelSuffix, entry)
	}
}

// TestPublicFinalizedOneShot verifies one-shot model output and failure output with finalized-message handlers.
func TestPublicFinalizedOneShot(t *testing.T) {
	t.Parallel()
	// Arrange the production executable once for all scenarios.
	binary := buildGlyphBinary(t)
	for _, test := range []struct {
		// name identifies the scenario.
		name string
		// mode selects the reference executable; empty means no extension.
		mode string
		// reject makes the provider reject the request.
		reject bool
		// stdout is the expected standard output; empty means a failure.
		stdout string
		// failures lists parts of the visible error output.
		failures []string
	}{
		{name: "no handlers", mode: "", reject: false, stdout: finalizedFinalText + "\n", failures: nil},
		{name: "redacted text", mode: "finalized-redact", reject: false, stdout: "[redacted]\n", failures: nil},
		{
			name: "invalid action", mode: "finalized-invalid", reject: false, stdout: "",
			failures: []string{
				"[error] EXTENSION_FAILED: run headless agent: ",
				`middleware handler "finalized-mark" returned invalid finalized-message action 0` + "\n",
			},
		},
		{
			name: "provider and finalization failure", mode: "finalized-fail-model", reject: true, stdout: "",
			failures: []string{
				"[error] EXTENSION_FAILED: run headless agent: ",
				`middleware handler "finalized-mark": ` + finalizedFailureText, finalizedProviderFailure,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a local provider endpoint, an isolated home, and the selected reference scenario.
			bodies := new(requestBodies)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, readErr := io.ReadAll(request.Body)
				if readErr != nil {
					t.Error(readErr)
					return
				}
				bodies.append(body)
				if test.reject {
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(http.StatusBadRequest)
					_, _ = writer.Write([]byte(`{"error":{"message":"` + finalizedProviderFailure + `"}}`))
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte(finalResponseSSE))
			}))
			t.Cleanup(server.Close)
			home := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(home, ".glyph"), 0o700))
			require.NoError(t, os.WriteFile(
				filepath.Join(home, ".glyph", "settings.yaml"), []byte(oneShotSettings(server.URL)), 0o600,
			))
			directory := t.TempDir()
			if test.mode != "" {
				directory = buildFinalizedFixture(t, test.mode)
			}

			// Act by running the production executable in one-shot mode.
			command := exec.CommandContext(
				t.Context(),
				binary,
				"run",
				"--extension-dir",
				directory,
				finalizedRequestText,
			)
			command.Env = append(os.Environ(), "HOME="+home)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			runErr := command.Run()

			// Assert the exit status, the model output, the visible category with complete text, and one request.
			assert.Len(t, bodies.snapshot(), 1)
			if test.stdout != "" {
				require.NoError(t, runErr, stderr.String())
				assert.Equal(t, test.stdout, stdout.String())
				return
			}
			exitErr, exited := errors.AsType[*exec.ExitError](runErr)
			require.True(t, exited, runErr)
			assert.Equal(t, 1, exitErr.ExitCode())
			for _, failure := range test.failures {
				assert.Contains(t, stderr.String(), failure)
			}
			assert.NotContains(t, stdout.String(), finalizedFinalText)
		})
	}
}

// TestPublicFinalizedBranchSummary verifies that a branch summary stores the final configured-model response.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport.
func TestPublicFinalizedBranchSummary(t *testing.T) {
	// Arrange one stored abandoned branch, the finalized-message reference, and a summary provider.
	paths := testPaths(t, retryModeSettings)
	writeProgrammaticCredentials(t, paths)
	seedSummaryControlSession(t, paths)
	transport, bodies := summaryRetryProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized"))
	defer fixture.closeOwner(t)
	sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetResumeSession(
			programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build(),
		)
	})

	// Act through one navigation with a branch summary.
	terminal := navigateWithSummary(t, fixture, "summary-finalized")

	// Assert the created summary contains the final response text.
	completed := terminal.GetEvent().GetCompleted()
	require.NotNil(t, completed, "failure: %v", terminal.GetEvent().GetFailed())
	assert.Equal(t, configuredResponseText+finalizedModelSuffix,
		completed.GetSessionTreeNavigation().GetCreatedSummary().GetBranchSummary().GetSummary())
	assert.Len(t, bodies.snapshot(), 2)
}
