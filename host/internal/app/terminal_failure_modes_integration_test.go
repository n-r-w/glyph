//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// terminalFailureUIBehavior selects the public UI failure observation fixture.
const terminalFailureUIBehavior = "terminal-failure"

// terminalFailureObservation retains a public category and its complete diagnostic text.
type terminalFailureObservation struct {
	// Code is the category received by the client.
	Code string `json:"code"`
	// Message is the diagnostic text received by the client.
	Message string `json:"error"`
}

// runTerminalFailureUIFixture records the public Submit operation's terminal failure.
func runTerminalFailureUIFixture(ctx context.Context, host *uisdk.Host) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("hello")}.Build())
	operation, err := host.Start(ctx, "terminal-submit", request)
	if err != nil {
		return err
	}
	_, err = waitUIOperation(ctx, host, "terminal-submit", operation, func(*uisdk.Notification) {})
	var failure *uisdk.FailureError
	if !errors.As(err, &failure) {
		return fmt.Errorf("expected UI failure: %w", err)
	}
	encoded, err := json.Marshal(terminalFailureObservation{Code: failure.Code(), Message: failure.Error()})
	if err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv(appUITraceEnvironment), encoded, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}

// TestTerminalRefusalAcrossPublicClients checks public error categories and causes after HTTP truncation.
func TestTerminalRefusalAcrossPublicClients(t *testing.T) {
	const codexProvider = "openai-codex"
	const contextOverflowCode = "context_length_exceeded"
	const contextLimitCategory = "CONTEXT_LIMIT"
	const cancelHandler, failureHandler, invalidHandler = "cancel", "fail", "invalid"
	const preserveHandler = "preserve"
	const canceledCategory, extensionCategory = "RETRY_CANCELED", "EXTENSION_FAILED"
	// Arrange the real extension executable and independent terminal scenarios.
	directory := buildPublicExtensionFixture(t)
	for _, scenario := range []struct {
		// name identifies the terminal source.
		name string
		// codex selects the concrete Codex driver for the focused refusal cases.
		codex bool
		// providerCode is the received terminal refusal classification.
		providerCode string
		// handler selects the public retry-handler action.
		handler string
		// category is the expected public failure code.
		category string
		// attempts is the number of provider attempts per logical request.
		attempts int64
	}{
		{
			name: "context overflow", codex: false, providerCode: "context_length_exceeded",
			handler: "", category: "CONTEXT_LIMIT", attempts: 1,
		},
		{
			name: "Codex context overflow", codex: true, providerCode: contextOverflowCode,
			handler: "", category: contextLimitCategory, attempts: 1,
		},
		{
			name: "overflow cancellation", codex: false, providerCode: contextOverflowCode,
			handler: cancelHandler, category: canceledCategory, attempts: 1,
		},
		{
			name: "overflow handler failure", codex: false, providerCode: contextOverflowCode,
			handler: failureHandler, category: extensionCategory, attempts: 1,
		},
		{
			name: "overflow invalid decision", codex: false, providerCode: contextOverflowCode,
			handler: invalidHandler, category: extensionCategory, attempts: 1,
		},
		{
			name: "overflow preserve", codex: false, providerCode: contextOverflowCode,
			handler: preserveHandler, category: contextLimitCategory, attempts: 1,
		},
		{
			name: "overflow repeat override", codex: false, providerCode: contextOverflowCode,
			handler: retryComposeMode, category: contextLimitCategory, attempts: 1,
		},
		{
			name: "non retryable", codex: false, providerCode: "invalid_request",
			handler: "", category: "MODEL_FAILED", attempts: 1,
		},
		{
			name: "exhaustion", codex: false, providerCode: "server_error",
			handler: "", category: "RETRY_EXHAUSTED", attempts: 2,
		},
		{
			name: "retry cancellation", codex: false, providerCode: "server_error",
			handler: "cancel", category: "RETRY_CANCELED", attempts: 1,
		},
		{
			name: "handler failure", codex: false, providerCode: "server_error",
			handler: "fail", category: "EXTENSION_FAILED", attempts: 1,
		},
		{
			name: "transport only", codex: false, providerCode: "",
			handler: "", category: "RETRY_EXHAUSTED", attempts: 2,
		},
	} {
		for _, client := range []string{"programmatic", "ui", "configured SDK", "headless"} {
			ownerlessHandler := scenario.providerCode == contextOverflowCode && scenario.handler != ""
			if (scenario.codex || ownerlessHandler) && client != "programmatic" && client != "configured SDK" {
				continue
			}
			t.Run(scenario.name+"/"+client, func(t *testing.T) {
				// Arrange a Responses endpoint that truncates the body, optionally after a terminal refusal.
				var attempts, outerRequests atomic.Int64
				reports := make(chan []byte, 1)
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					input, err := io.ReadAll(request.Body)
					if err != nil {
						t.Error(err)
						return
					}
					writer.Header().Set("Content-Type", "text/event-stream")
					var payload struct {
						// Model identifies the selected provider model.
						Model string `json:"model"`
					}
					if err := json.Unmarshal(input, &payload); err != nil {
						t.Error(err)
						return
					}
					if client == "configured SDK" && payload.Model != "gpt-test" {
						if outerRequests.Add(1) == 1 {
							_, _ = writer.Write([]byte(configuredTerminalToolSSE))
						} else {
							reports <- input
							_, _ = writer.Write([]byte(finalResponseSSE))
						}
						return
					}
					attempt := attempts.Add(1)
					body := "data: " + `{"type":"response.output_text.delta",` +
						`"output_index":0,"content_index":0,"delta":"partial"}` + "\n\n"
					if scenario.providerCode != "" {
						body += fmt.Sprintf(
							"data: "+`{"type":"response.failed","response":{"id":"rejected","model":"demo",`+
								`"status":"failed","output":[],"error":{"code":%q,`+
								`"message":"provider terminal rejection full unique text attempt %d"}}}`+"\n\n",
							scenario.providerCode,
							attempt,
						)
					}
					writer.Header().Set("Content-Length", fmt.Sprint(len(body)+100))
					_, _ = writer.Write([]byte(body))
				}))
				t.Cleanup(server.Close)
				compatible := strings.Replace(
					strings.SplitN(configuredRequestSettings, "providers:\n", 2)[1],
					"  openai-codex:",
					"  local:",
					1,
				)
				compatible = strings.Replace(
					compatible,
					"type: openai-codex",
					"type: openai-compatible\n    api: responses\n    baseURL: "+server.URL,
					1,
				)
				settings := strings.Replace(
					retryModeSettings,
					"defaultProvider: openai-codex",
					"defaultProvider: local",
					1,
				)
				settings = strings.Replace(settings, "providers:\n", "providers:\n"+compatible, 1)
				configuredProvider := "local"
				if scenario.codex {
					settings = retryModeSettings
					configuredProvider = codexProvider
					endpoint, err := url.Parse(server.URL)
					require.NoError(t, err)
					previous := http.DefaultTransport
					transport := NewMockHTTPRoundTripper(gomock.NewController(t))
					transport.EXPECT().
						RoundTrip(gomock.Any()).
						DoAndReturn(func(request *http.Request) (*http.Response, error) {
							forwarded := request.Clone(request.Context())
							forwarded.URL.Scheme = endpoint.Scheme
							forwarded.URL.Host = endpoint.Host
							return previous.RoundTrip(forwarded)
						}).
						AnyTimes()
					http.DefaultTransport = transport
					t.Cleanup(func() { http.DefaultTransport = previous })
				}
				paths := testPaths(t, settings)
				writeProgrammaticCredentials(t, paths)
				signals := t.TempDir()
				t.Setenv("GLYPH_EXTERNAL_SIGNALS", signals)
				t.Setenv("GLYPH_EXTERNAL_RETRY_HANDLER", scenario.handler)
				t.Setenv("GLYPH_EXTERNAL_CONFIGURED_PROVIDER", configuredProvider)
				var observations []terminalFailureObservation
				retryProgress := 0

				// Act through the production client assembly and terminal-error operation.
				switch client {
				case "programmatic":
					fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
					require.NoError(t, fixture.stream.Send(userRequest("terminal-refusal", "hello")))
					for len(observations) == 0 {
						response, err := fixture.stream.Recv()
						require.NoError(t, err)
						event := response.GetEvent()
						if event.GetProgress().GetAgentEvent().GetRetryProgress() != nil {
							retryProgress++
						}
						if failure := event.GetFailed(); failure != nil {
							observations = append(
								observations,
								terminalFailureObservation{Code: failure.GetCode(), Message: failure.GetMessage()},
							)
						}
						require.False(
							t,
							event.HasCompleted() || event.HasCanceled(),
							"unexpected successful or canceled terminal",
						)
					}
					fixture.closeOwner(t)
				case "ui":
					trace := filepath.Join(t.TempDir(), "terminal.json")
					t.Setenv(appUIBehaviorEnvironment, terminalFailureUIBehavior)
					t.Setenv(appUITraceEnvironment, trace)
					uiDirectory := t.TempDir()
					writeUIExecutable(t, uiDirectory, "Terminal_UI")
					require.NoError(t, runWithPaths(t.Context(), paths, cli.Command{
						Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: directory,
						UIDirectory: uiDirectory, UIID: "terminal-ui", SocketPath: "",
					}, &bytes.Buffer{}, &bytes.Buffer{}))
					encoded, err := os.ReadFile(trace)
					require.NoError(t, err)
					var observation terminalFailureObservation
					require.NoError(t, json.Unmarshal(encoded, &observation))
					observations = append(observations, observation)
				case "configured SDK":
					fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
					completeProgrammaticRequest(t, fixture, userRequest("configured-terminal", "hello"))
					fixture.closeOwner(t)
					var report struct {
						// Wait records the Wait terminal error.
						Wait terminalFailureObservation `json:"wait"`
						// WaitWithProgress records the progress-wait terminal error.
						WaitWithProgress terminalFailureObservation `json:"wait_with_progress"`
						// ProgressCount records delivered retry updates.
						ProgressCount int `json:"progress_count"`
					}
					require.EqualValues(
						t,
						2,
						outerRequests.Load(),
						"configured operation must return its report to the provider",
					)
					require.NoError(t, json.Unmarshal([]byte(externalToolOutput(t, <-reports)), &report))
					observations = append(observations, report.Wait, report.WaitWithProgress)
					require.Len(t, observations, 2)
					retryProgress = report.ProgressCount
				case "headless":
					err := runWithPaths(t.Context(), paths, cli.Command{
						Mode:               cli.ModeHeadless,
						Headless:           headless.Command{UserText: "hello", ExtensionDirectory: directory},
						ExtensionDirectory: directory,
						UIDirectory:        "",
						UIID:               "",
						SocketPath:         "",
					}, &bytes.Buffer{}, &bytes.Buffer{})
					require.Error(t, err)
					var failure interface {
						error
						FailureCode() string
					}
					require.ErrorAs(t, err, &failure)
					observations = append(
						observations,
						terminalFailureObservation{Code: failure.FailureCode(), Message: err.Error()},
					)
				}

				// Assert the public error category, acquired causes, and provider request count.
				expectedAttempts := scenario.attempts
				if client == "configured SDK" {
					expectedAttempts *= 2
				}
				assert.Equal(t, expectedAttempts, attempts.Load())
				if ownerlessHandler {
					assert.FileExists(t, filepath.Join(signals, retryInvokedSignal))
					assert.Zero(t, retryProgress)
				}

				for index, observation := range observations {
					assert.Equal(t, scenario.category, observation.Code)
					assert.Contains(t, observation.Message, io.ErrUnexpectedEOF.Error())
					if scenario.providerCode != "" {
						first := int64(index)*scenario.attempts + 1
						for attempt := first; attempt < first+scenario.attempts; attempt++ {
							assert.Contains(
								t,
								observation.Message,
								fmt.Sprintf("provider terminal rejection full unique text attempt %d", attempt),
							)
						}
					}
					if ownerlessHandler && scenario.handler == invalidHandler {
						assert.Contains(t, observation.Message, "negative delay")
					}
					if scenario.handler == "fail" {
						assert.Contains(t, observation.Message, "retry handler full unique failure text")
					}
				}
			})
		}
	}
}

// configuredTerminalToolSSE requests both configured-model wait APIs through the external tool.
const configuredTerminalToolSSE = `data: {"type":"response.completed","response":{"id":"tool-request",` +
	`"status":"completed","output":[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"external",` +
	`"arguments":"{\"mode\":\"configured-request-failures\"}","status":"completed"}]}}` + "\n\n"
