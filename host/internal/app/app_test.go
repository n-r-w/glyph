//nolint:exhaustruct // Protobuf oneof builders intentionally set only the active field.
package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"google.golang.org/grpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	appUIHelperEnvironment   = "GLYPH_APP_UI_HELPER"
	appUITraceEnvironment    = "GLYPH_APP_UI_TRACE"
	appUITerminalEnvironment = "GLYPH_APP_UI_TERMINAL"
	appUIBehaviorEnvironment = "GLYPH_APP_UI_BEHAVIOR"
	appUIPTYInnerEnvironment = "GLYPH_APP_PTY_INNER"
)

// TestNewProviderCatalogBuildsEveryConfiguredProvider verifies deterministic composition and defaults.
func TestNewProviderCatalogBuildsEveryConfiguredProvider(t *testing.T) {
	t.Parallel()

	environment := "COMPATIBLE_API_KEY"
	configured := settingstore.Settings{
		DefaultProvider: "a-compatible",
		DefaultModel:    "a-second",
		Providers: map[string]settingstore.Provider{
			"openai-codex": {
				Type: settingstore.ProviderTypeOpenAICodex,
				Models: []settingstore.Model{
					{ID: "codex-first", Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceOff)},
					{ID: "codex-second", Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceLow)},
				},
			},
			"z-compatible": {
				Type: settingstore.ProviderTypeOpenAICompatible, BaseURL: "http://localhost:11434/v1",
				API: settingstore.APIChatCompletions,
				Models: []settingstore.Model{{
					ID: "z-model", Reasoning: settingstore.Reasoning{
						Supported:  true,
						Choices:    []settingstore.ReasoningChoice{settingstore.ReasoningChoiceOn},
						Default:    settingstore.ReasoningChoiceOn,
						WireFormat: settingstore.ReasoningWireFormatOllamaOrnith,
					},
				}},
			},
			"a-compatible": {
				Type: settingstore.ProviderTypeOpenAICompatible, BaseURL: "https://example.com/v1",
				API:    settingstore.APIChatCompletions,
				APIKey: &settingstore.APIKey{Environment: &environment},
				Models: []settingstore.Model{
					{ID: "a-first", Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceOff)},
					{ID: "a-second", API: settingstore.APIResponses, Reasoning: testSettingsReasoning(settingstore.ReasoningChoiceLow, settingstore.ReasoningChoiceHigh)},
				},
			},
		},
	}
	paths := persistence.Paths{CredentialsFile: filepath.Join(t.TempDir(), "credentials.json")}

	catalog, err := newProviderCatalog(configured, paths, interactions.New(), hookrunner.New(nil, nil, nil))

	require.NoError(t, err)
	models := catalog.Models()
	require.Len(t, models, 5)
	assert.Equal(t, []string{
		"a-compatible/a-first", "a-compatible/a-second", "openai-codex/codex-first",
		"openai-codex/codex-second", "z-compatible/z-model",
	}, []string{
		string(models[0].Provider) + "/" + string(models[0].Model),
		string(models[1].Provider) + "/" + string(models[1].Model),
		string(models[2].Provider) + "/" + string(models[2].Model),
		string(models[3].Provider) + "/" + string(models[3].Model),
		string(models[4].Provider) + "/" + string(models[4].Model),
	})
	assert.Equal(t, model.Selection{
		Provider: "a-compatible", Model: "a-second", ReasoningChoice: model.ReasoningChoiceHigh,
	}, catalog.Selection())
	assert.Equal(t, model.ProviderID("a-compatible"), catalog.Current().Model.Provider)
	assert.Equal(t, model.ReasoningCapabilities{
		Supported: true,
		Choices:   []model.ReasoningChoice{model.ReasoningChoiceOn},
		Default:   model.ReasoningChoiceOn,
	}, models[4].ReasoningCapabilities)
}

// appUIService records initialization and terminates through one quit command.
type appUIService struct {
	uipb.UnimplementedUIServiceServer
}

// TestUIPluginHelperProcess serves the fake UI when this test binary is a child process.
func TestUIPluginHelperProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv(appUIHelperEnvironment) == "serve" {
		uisdk.Serve(&appUIService{
			UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		})
	}
}

// GetCapabilities declares a non-terminal fake UI for application composition tests.
func (*appUIService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	controlsTerminal := os.Getenv(appUITerminalEnvironment) == "1"
	if os.Getenv(appUIBehaviorEnvironment) == "snapshot" {
		_ = os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	return uipb.GetCapabilitiesResponse_builder{ControlsTerminal: new(controlsTerminal)}.Build(), nil
}

// Open records the first frame and sends the authoritative quit command.
func (*appUIService) Open(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	frame, err := stream.Recv()
	if err != nil {
		return err
	}
	initialization := frame.GetInitialization()
	startupText := make([]string, 0, len(initialization.GetStartupContent()))
	for _, content := range initialization.GetStartupContent() {
		startupText = append(startupText, content.GetText())
	}
	trace := fmt.Sprintf(
		"%d\n%s\n%s\n",
		os.Getpid(), initialization.GetSelectedUiId(), strings.Join(startupText, "\n"),
	)
	if os.Getenv(appUIBehaviorEnvironment) != "semantic" {
		if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(trace), 0o600); err != nil {
			return err
		}
	}
	if os.Getenv(appUITerminalEnvironment) == "1" {
		terminalFile, terminalErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if terminalErr != nil {
			return terminalErr
		}
		if _, terminalErr = term.MakeRaw(terminalFile.Fd()); terminalErr != nil {
			return terminalErr
		}
		_, terminalErr = terminalFile.WriteString(
			ansi.SetMode(ansi.ModeAltScreenSaveCursor, ansi.ModeBracketedPaste) + ansi.HideCursor,
		)
		if terminalErr != nil {
			return terminalErr
		}
	}
	if os.Getenv(appUIBehaviorEnvironment) == "crash" {
		os.Exit(23)
	}
	if os.Getenv(appUIBehaviorEnvironment) == "authentication" {
		for {
			frame, receiveErr := stream.Recv()
			if receiveErr != nil {
				return receiveErr
			}
			lifecycle := frame.GetLifecycle()
			if lifecycle == nil || lifecycle.GetType() != uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
				continue
			}
			availability := lifecycle.GetAvailability()
			if availability != uipb.Availability_AVAILABILITY_IDLE &&
				availability != uipb.Availability_AVAILABILITY_AUTHENTICATING &&
				availability != uipb.Availability_AVAILABILITY_AUTHENTICATION_FAILED {
				continue
			}
			if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(availability.String()), 0o600); err != nil {
				return err
			}
			return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
		}
	}
	//nolint:nestif // The helper serves one explicit lifecycle mode for this process fixture.
	if os.Getenv(appUIBehaviorEnvironment) == "semantic" {
		if err := stream.Send(uipb.OpenResponse_builder{Submit: uipb.SubmitCommand_builder{Text: new("read input.txt")}.Build()}.Build()); err != nil {
			return err
		}
		file, err := os.OpenFile(os.Getenv(appUITraceEnvironment), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				slog.ErrorContext(stream.Context(), "close semantic UI trace", "error", closeErr)
			}
		}()
		settled := false
		for {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			if lifecycle := frame.GetLifecycle(); lifecycle != nil {
				payload, marshalErr := json.Marshal(map[string]any{
					"type": lifecycle.GetType(), "text": lifecycle.GetText(),
					"model_text": lifecycle.GetModelResponse().GetText(),
					"tool_name":  lifecycle.GetToolName(), "tool_status": !lifecycle.GetIsError(),
					"outcome": lifecycle.GetOutcome(), "settled": lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
					"availability":         lifecycle.GetAvailability(),
					"tool_result_contents": semanticToolResultContents(lifecycle.GetToolResultContents()),
				})
				if marshalErr != nil {
					return marshalErr
				}
				if _, writeErr := fmt.Fprintf(file, "%s\n", payload); writeErr != nil {
					return writeErr
				}
				if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED {
					settled = true
				}
				if settled && lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED && lifecycle.GetAvailability() == uipb.Availability_AVAILABILITY_IDLE {
					return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
				}
			}
		}
	}
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

// semanticToolResultContents keeps typed result blocks stable in the shared lifecycle fixture.
func semanticToolResultContents(contents []*uipb.ToolResultContent) []map[string]any {
	mapped := make([]map[string]any, 0, len(contents))
	for _, content := range contents {
		switch content.WhichContent() {
		case uipb.ToolResultContent_Text_case:
			mapped = append(mapped, map[string]any{"text": content.GetText()})
		case uipb.ToolResultContent_Image_case:
			image := content.GetImage()
			mapped = append(mapped, map[string]any{"image": map[string]any{
				"media_type": image.GetMediaType(), "data": image.GetData(),
			}})
		case uipb.ToolResultContent_Content_not_set_case:
			continue
		}
	}
	return mapped
}

// TestRunHeadlessUsesCompatibleDefaultWithoutAuthorization verifies the default runtime and keyless request.
func TestRunHeadlessUsesCompatibleDefaultWithoutAuthorization(t *testing.T) {
	t.Parallel()

	requestCount := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		assert.Equal(t, "/v1/chat/completions", request.URL.Path)
		assert.Empty(t, request.Header.Values("Authorization"))
		writer.Header().Set("Content-Type", "text/event-stream")
		_, err := io.WriteString(writer, "data: {\"id\":\"chat-1\",\"model\":\"local-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"compatible response\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
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
        reasoning:
          supported: false
          choices: [off]
          default: off
`, server.URL))
	var stdout, stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless, Headless: headless.Command{UserText: "request", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, int32(1), requestCount.Load())
	assert.Equal(t, "compatible response\n", stdout.String())
}

// TestRunWithPathsUIInvalidSettingsStopsBeforeLogging verifies validation precedes UI startup side effects.
func TestRunWithPathsUIInvalidSettingsStopsBeforeLogging(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: codex-model\n")
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: t.TempDir(), UIID: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.ErrorContains(t, err, "load Glyph settings")
	_, statErr := os.Stat(paths.LogFile)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestRunWithPathsUICodexDefaultKeepsProviderAuthentication verifies Codex-owned startup authentication.
func TestRunWithPathsUICodexDefaultKeepsProviderAuthentication(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Codex_UI")
	tracePath := filepath.Join(t.TempDir(), "authentication-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "authentication")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "codex-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	payload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	assert.Equal(t, uipb.Availability_AVAILABILITY_AUTHENTICATING.String(), string(payload))
}

// TestRunWithPathsUICompatibleDefaultSkipsCodexAuthentication verifies active-provider startup authentication.
func TestRunWithPathsUICompatibleDefaultSkipsCodexAuthentication(t *testing.T) {
	paths := testPaths(t, `defaultProvider: local
defaultModel: local-model
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: codex-model
        reasoning:
          supported: false
          choices: [off]
          default: off
  local:
    type: openai-compatible
    baseURL: http://localhost:11434/v1
    api: chat-completions
    models:
      - id: local-model
        reasoning:
          supported: false
          choices: [off]
          default: off
`)
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Compatible_UI")
	tracePath := filepath.Join(t.TempDir(), "authentication-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "authentication")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "compatible-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	payload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	assert.Equal(t, uipb.Availability_AVAILABILITY_IDLE.String(), string(payload))
}

// TestRunWithPathsIgnoresActiveUIAndFailsWithoutCredentials verifies headless-only concrete composition.
func TestRunWithPathsIgnoresActiveUIAndFailsWithoutCredentials(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings("activeUI: UI__DO_NOT_TOUCH\n"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeHeadless,
		Headless:           headless.Command{UserText: "request", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &stdout, &stderr)

	require.ErrorContains(t, err, "sign-in required")
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "[info] headless")
	assert.Contains(t, stderr.String(), "[info] extensions: none")
	assert.NotContains(t, stderr.String(), "ui-do-not-touch")
}

// TestRunWithPathsRejectsInvalidExplicitExtensionDirectory verifies invocation override failure.
func TestRunWithPathsRejectsInvalidExplicitExtensionDirectory(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	missingDirectory := filepath.Join(t.TempDir(), "missing-extensions")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeHeadless,
		Headless:           headless.Command{UserText: "request", ExtensionDirectory: missingDirectory},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	assert.ErrorContains(t, err, "explicit extension directory")
}

// TestRunWithPathsReportsUnreadableDefaultDirectory verifies unreadable defaults remain startup diagnostics.
func TestRunWithPathsReportsUnreadableDefaultDirectory(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	extensionDirectory := filepath.Join(paths.Directory, "plugins", "extension")
	require.NoError(t, os.MkdirAll(extensionDirectory, 0o700))
	require.NoError(t, os.Chmod(extensionDirectory, 0o000))
	t.Cleanup(func() { require.NoError(t, os.Chmod(extensionDirectory, 0o700)) })
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeHeadless,
		Headless:           headless.Command{UserText: "request", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "sign-in required")
	assert.Contains(t, stderr.String(), "[extension:error]")
	assert.Contains(t, stderr.String(), "[info] extensions: none")
}

// TestRunWithPathsUIReportsAutomaticSelectionWarnings preserves structured failed-selection diagnostics.
func TestRunWithPathsUIReportsAutomaticSelectionWarnings(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "no compatible UI plugin is available")
	assert.Contains(t, stderr.String(), "[warning] excluded UI broken-ui at "+brokenPath+":")
	assert.Contains(t, stderr.String(), "start UI \"broken-ui\"")
}

// TestRunWithPathsUIReportsSelectionWarningsBeforeExtensionStartupFailure preserves pending diagnostics.
func TestRunWithPathsUIReportsSelectionWarningsBeforeExtensionStartupFailure(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	writeUIExecutable(t, uiDirectory, "Valid_UI")
	missingExtensionDirectory := filepath.Join(t.TempDir(), "missing-extensions")
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: missingExtensionDirectory, UIDirectory: uiDirectory, UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "explicit extension directory")
	warning := "[warning] excluded UI broken-ui at " + brokenPath + ":"
	assert.Equal(t, 1, strings.Count(stderr.String(), warning))
	assert.Contains(t, stderr.String(), "start UI \"broken-ui\"")
}

// TestRunWithPathsUIKeepsSelectionWarningsInInitialization prevents duplicate terminal diagnostics.
func TestRunWithPathsUIKeepsSelectionWarningsInInitialization(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	writeUIExecutable(t, uiDirectory, "Valid_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.NoError(t, err)
	trace, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	warning := "excluded UI broken-ui at " + brokenPath + ":"
	assert.Contains(t, string(trace), warning)
	assert.NotContains(t, stderr.String(), warning)
}

// TestRunWithPathsUIUsesSelectedStreamAndCleansProcess verifies real UI process composition.
func TestRunWithPathsUIUsesSelectedStreamAndCleansProcess(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Fake_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "fake-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	trace, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(trace)), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "fake-ui", lines[1])
	assert.Contains(t, lines[2], "UI fake-ui")
	processID, err := strconv.Atoi(lines[0])
	require.NoError(t, err)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
	logPayload, err := os.ReadFile(paths.LogFile)
	require.NoError(t, err)
	assert.Contains(t, string(logPayload), "starting UI Glyph application")
	assert.Contains(t, string(logPayload), "loading UI plugins")
	assert.Contains(t, string(logPayload), "loaded UI plugin")
	assert.Contains(t, string(logPayload), `"plugin_id":"fake-ui"`)
	assert.Contains(t, string(logPayload), `"controls_terminal":false`)
	assert.Contains(t, string(logPayload), "loading extensions")
	assert.Contains(t, string(logPayload), "loaded extensions")
}

// TestRunWithPathsUITerminalSnapshotFailureStopsBeforeOpen verifies terminal capture is a startup gate.
func TestRunWithPathsUITerminalSnapshotFailureStopsBeforeOpen(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Terminal_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUITerminalEnvironment, "1")
	t.Setenv(appUIBehaviorEnvironment, "snapshot")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "terminal-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	require.ErrorContains(t, err, "capture selected UI terminal")
	payload, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(string(payload))
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}

// TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement verifies abnormal stream authority.
func TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Crash_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "crash")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "crash-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	require.ErrorContains(t, err, "receive UI command")
	trace, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(strings.Split(strings.TrimSpace(string(trace)), "\n")[0])
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}

// TestHostSemanticClientMatchesHeadlessOutcome verifies shared public semantics.
func TestHostSemanticClientMatchesHeadlessOutcome(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(fmt.Sprintf(`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken)), 0o600))
	requestCount := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = deterministicCodexTransport{requestCount: requestCount}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	extensionDirectory := buildToolsExecutable(t)
	var headlessStdout, headlessStderr bytes.Buffer
	headlessErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless, Headless: headless.Command{UserText: "read input.txt", ExtensionDirectory: extensionDirectory},
	}, &headlessStdout, &headlessStderr)
	require.NoError(t, headlessErr)
	assert.Equal(t, int32(2), requestCount.Load())
	headlessObservation := parseHeadlessOutcome(headlessStdout.String(), headlessStderr.String(), headlessErr)
	expected := sharedOutcome{FinalText: "Request complete.", ToolName: "bash", ToolStartName: "bash", ToolEndName: "bash", ToolStatus: "ok", ToolStarted: true, ToolEnded: true, CommandSucceeded: true}
	assert.Equal(t, expected, headlessObservation)

	requestCount.Store(0)
	uiDirectory := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "semantic-ui.jsonl")
	t.Setenv(appUIBehaviorEnvironment, "semantic")
	t.Setenv(appUITraceEnvironment, tracePath)
	writeUIExecutable(t, uiDirectory, "Semantic_UI")
	uiErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: extensionDirectory},
		ExtensionDirectory: extensionDirectory, UIDirectory: uiDirectory, UIID: "semantic-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, uiErr)
	assert.Equal(t, int32(2), requestCount.Load())
	ui := parseUIObservation(t, tracePath)
	assert.Equal(t, loadSemanticLifecycle(t), ui.Records)
	assert.Equal(t, expected, ui.Shared)
	assert.Equal(t, headlessObservation, ui.Shared)
}

// sharedOutcome contains only fields observable from both Host client boundaries.
type sharedOutcome struct {
	FinalText, ToolName, ToolStatus string
	ToolStartName, ToolEndName      string
	ToolStarted, ToolEnded          bool
	CommandSucceeded                bool
}

// semanticLifecycleRecord is the stable subset shared with the standard consumer fixture.
type semanticLifecycleRecord struct {
	Type               string          `json:"type"`
	ToolName           string          `json:"tool_name,omitempty"`
	ToolStatus         string          `json:"tool_status,omitempty"`
	Text               string          `json:"text,omitempty"`
	ToolResultContents json.RawMessage `json:"tool_result_contents,omitempty"`
	ModelText          string          `json:"model_text,omitempty"`
	Outcome            string          `json:"outcome,omitempty"`
	Availability       string          `json:"availability,omitempty"`
}

// uiObservation stores normalized UI lifecycle records and its derived public outcome.
type uiObservation struct {
	Shared  sharedOutcome
	Records []semanticLifecycleRecord
}

// parseHeadlessOutcome reads shared fields from the one-shot public output.
func parseHeadlessOutcome(stdout, stderr string, err error) sharedOutcome {
	observation := sharedOutcome{FinalText: strings.TrimSpace(stdout), CommandSucceeded: err == nil}
	for _, line := range strings.Split(stderr, "\n") {
		switch {
		case strings.HasPrefix(line, "[tool:start] "):
			observation.ToolName = strings.TrimPrefix(line, "[tool:start] ")
			observation.ToolStartName = observation.ToolName
			observation.ToolStarted = true
		case strings.HasPrefix(line, "[tool:end] "):
			parts := strings.SplitN(strings.TrimPrefix(line, "[tool:end] "), ": ", 2)
			if len(parts) == 2 {
				observation.ToolName, observation.ToolStatus = parts[0], parts[1]
				observation.ToolEndName = parts[0]
				observation.ToolEnded = true
			}
		}
	}
	return observation
}

// parseUIObservation validates and normalizes every semantic UI trace line.
func parseUIObservation(t *testing.T, path string) uiObservation {
	t.Helper()
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var records []semanticLifecycleRecord
	var finalText, toolName, toolStatus string
	var toolStarted, toolEnded, agentCompleted, settled bool
	var toolStartName, toolEndName string
	for _, line := range strings.Split(strings.TrimSpace(string(payload)), "\n") {
		var item struct {
			Type               uipb.LifecycleType `json:"type"`
			Text               string             `json:"text"`
			ModelText          string             `json:"model_text"`
			ToolName           string             `json:"tool_name"`
			ToolStatus         bool               `json:"tool_status"`
			Outcome            string             `json:"outcome"`
			Availability       uipb.Availability  `json:"availability"`
			ToolResultContents json.RawMessage    `json:"tool_result_contents"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &item))
		record := semanticLifecycleRecord{}
		//nolint:exhaustive // The fixture keeps only lifecycle fields used by the semantic consumer.
		switch item.Type {
		case uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_START:
			record.Type = "agent_start"
		case uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
			record.Type, record.ModelText = "message_end", item.ModelText
			finalText = item.ModelText
		case uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
			record.Type, record.ToolName = "tool_execution_start", item.ToolName
			toolStartName = item.ToolName
			toolStarted = true
		case uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
			record.Type, record.ToolName = "tool_execution_end", item.ToolName
			toolEndName = item.ToolName
			toolEnded = true
			if item.ToolStatus {
				record.ToolStatus = "ok"
			} else {
				record.ToolStatus = "error"
			}
			toolName, toolStatus = item.ToolName, record.ToolStatus
		case uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
			record.Type, record.ToolName = "tool_result", item.ToolName
			record.ToolResultContents = item.ToolResultContents
		case uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
			record.Type, record.Outcome = "agent_end", item.Outcome
			agentCompleted = item.Outcome == "completed"
		case uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
			record.Type = "agent_settled"
			settled = true
		case uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
			if !settled || item.Availability != uipb.Availability_AVAILABILITY_IDLE {
				continue
			}
			record.Type, record.Availability = "availability", "idle"
		default:
			continue
		}
		records = append(records, record)
		if item.ToolName != "" {
			toolName = item.ToolName
		}
	}
	return uiObservation{Records: records, Shared: sharedOutcome{
		FinalText: finalText, ToolName: toolName, ToolStatus: toolStatus,
		ToolStartName: toolStartName, ToolEndName: toolEndName,
		ToolStarted: toolStarted, ToolEnded: toolEnded, CommandSucceeded: agentCompleted && settled,
	}}
}

// loadSemanticLifecycle provides the exact sequence consumed by the TUI mapping test.
func loadSemanticLifecycle(t *testing.T) []semanticLifecycleRecord {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "semantic-ui-lifecycle.json"))
	require.NoError(t, err)
	var records []semanticLifecycleRecord
	require.NoError(t, json.Unmarshal(payload, &records))
	return records
}

// deterministicCodexTransport returns two fixed responses and never performs network I/O.
type deterministicCodexTransport struct{ requestCount *atomic.Int32 }

func (transport deterministicCodexTransport) RoundTrip(*http.Request) (*http.Response, error) {
	requestNumber := transport.requestCount.Add(1)
	switch requestNumber {
	case 1:
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(toolResponseSSE)), Header: make(http.Header)}, nil
	case 2:
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(finalResponseSSE)), Header: make(http.Header)}, nil
	default:
		return nil, errors.New("deterministic Codex transport received more than two requests")
	}
}

const toolResponseSSE = `data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"bash","arguments":"","status":"in_progress"}}

data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc-1","name":"bash","arguments":"{\"command\":\"printf tool-ok\"}"}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"bash","arguments":"{\"command\":\"printf tool-ok\"}","status":"completed"}}

data: {"type":"response.completed","response":{"id":"resp-1","status":"completed","output":[]}}

data: [DONE]

`
const finalResponseSSE = `data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Request complete."}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Request complete.","annotations":[],"logprobs":[]}]}}

data: {"type":"response.completed","response":{"id":"resp-2","status":"completed","output":[]}}

data: [DONE]

`

// semanticAccessToken creates credentials accepted by the local deterministic provider path.
func semanticAccessToken(t *testing.T, accountID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": accountID}})
	require.NoError(t, err)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// buildToolsExecutable compiles the real tools command into a test-owned temporary directory.
func buildToolsExecutable(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	output := filepath.Join(t.TempDir(), "glyph-tools")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", output, "./plugins/extension/tools/cmd/glyph-tools")
	command.Dir = root
	outputBytes, err := command.CombinedOutput()
	require.NoError(t, err, string(outputBytes))
	return filepath.Dir(output)
}

// repoRoot resolves the repository from the test package working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(filepath.Join(directory, "..", "..", ".."))
}

// TestTerminalRecoveryPTY proves normal and os.Exit(23) recovery against a real Darwin PTY.
func TestTerminalRecoveryPTY(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"normal", "crash"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			command := exec.CommandContext(
				t.Context(), "/usr/bin/script", "-q", "/dev/null",
				os.Args[0], "-test.run=^TestTerminalRecoveryPTYInner$",
			)
			command.Env = append(os.Environ(), appUIPTYInnerEnvironment+"="+mode)
			output, err := command.CombinedOutput()
			require.NoError(t, err, string(output))
			assert.Contains(t, string(output), "PASS")
		})
	}
}

// TestTerminalRecoveryPTYInner mutates and verifies one controlling-terminal lifecycle.
func TestTerminalRecoveryPTYInner(t *testing.T) {
	t.Parallel()

	mode := os.Getenv(appUIPTYInnerEnvironment)
	if mode == "" {
		return
	}
	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	originalState := terminalState(t, terminalFile)
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	writeConfiguredUIExecutable(t, uiDirectory, "Terminal_UI", tracePath, mode)

	runErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "terminal-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if mode == "crash" {
		require.Error(t, runErr)
	} else {
		require.NoError(t, runErr)
	}
	assert.Equal(t, normalizeTerminalState(originalState), normalizeTerminalState(terminalState(t, terminalFile)))
}

// terminalState reads the exact controlling-terminal termios representation.
func terminalState(t *testing.T, terminalFile *os.File) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "stty", "-g")
	command.Stdin = terminalFile
	output, err := command.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(output))
}

// normalizeTerminalState ignores macOS's transient PENDIN bit after successful restoration.
func normalizeTerminalState(state string) string {
	parts := strings.Split(state, ":")
	for index, part := range parts {
		if !strings.HasPrefix(part, "lflag=") {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(part, "lflag="), 16, 64)
		if err != nil {
			return state
		}
		parts[index] = "lflag=" + strconv.FormatUint(value&^uint64(syscall.PENDIN), 16)
	}
	return strings.Join(parts, ":")
}

// writeUIExecutable creates one executable wrapper around the current test binary.
func writeUIExecutable(t *testing.T, directory, name string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=serve exec %q -test.run=^TestUIPluginHelperProcess$\n",
		appUIHelperEnvironment,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o755))
}

// writeConfiguredUIExecutable embeds one isolated PTY helper configuration.
func writeConfiguredUIExecutable(t *testing.T, directory, name, tracePath, mode string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=serve %s=%q %s=1 %s=%q exec %q -test.run=^TestUIPluginHelperProcess$\n",
		appUIHelperEnvironment,
		appUITraceEnvironment,
		tracePath,
		appUITerminalEnvironment,
		appUIBehaviorEnvironment,
		mode,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o755))
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
		Directory: directory, SettingsFile: settingsPath,
		CredentialsFile: filepath.Join(directory, "credentials.json"),
		LogsDirectory:   logsDirectory, LogFile: filepath.Join(logsDirectory, "glyph.log"),
	}
}

func testSettingsReasoning(choices ...settingstore.ReasoningChoice) settingstore.Reasoning {
	supported := len(choices) != 1 || choices[0] != settingstore.ReasoningChoiceOff
	wireFormat := settingstore.ReasoningWireFormat("")
	if supported {
		wireFormat = settingstore.ReasoningWireFormatOpenAIResponses
	}
	return settingstore.Reasoning{
		Supported: supported, Choices: choices, Default: choices[len(choices)-1], WireFormat: wireFormat,
	}
}
