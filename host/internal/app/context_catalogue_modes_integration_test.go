//go:build integration

package app

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestPublicContextCataloguesAcrossApplicationModes runs the same public-only extension through all Host assemblies.
func TestPublicContextCataloguesAcrossApplicationModes(t *testing.T) {
	// Arrange: build the public-only extension once and replace only the provider HTTP adapter.
	directory := buildPublicCatalogueExtension(t)
	for _, scenario := range []struct {
		// name identifies the application assembly under test.
		name string
		// mode selects the public client contract.
		mode cli.Mode
	}{
		{name: "headless", mode: cli.ModeHeadless},
		{name: "ui", mode: cli.ModeUI},
		{name: "programmatic", mode: cli.ModeRPC},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			var count atomic.Int32
			var body atomic.Value
			previous := http.DefaultTransport
			http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string { return "catalogs" })
			t.Cleanup(func() { http.DefaultTransport = previous })

			// Act: let Agent Core invoke the external tool, which reads both catalogs through its context.
			switch scenario.mode {
			case cli.ModeHeadless:
				err := runWithPaths(t.Context(), paths, cli.Command{
					Mode:               cli.ModeHeadless,
					Headless:           headless.Command{UserText: "inspect catalog", ExtensionDirectory: directory},
					ExtensionDirectory: "", UIDirectory: "", UIID: "", SocketPath: "",
				}, &bytes.Buffer{}, &bytes.Buffer{})
				require.NoError(t, err)
			case cli.ModeUI:
				uiDirectory := t.TempDir()
				t.Setenv(appUIBehaviorEnvironment, "semantic")
				t.Setenv(appUITraceEnvironment, filepath.Join(t.TempDir(), "ui.jsonl"))
				writeUIExecutable(t, uiDirectory, "Semantic_UI")
				err := runWithPaths(t.Context(), paths, cli.Command{
					Mode: cli.ModeUI, Headless: headless.Command{},
					ExtensionDirectory: directory, UIDirectory: uiDirectory, UIID: "semantic-ui", SocketPath: "",
				}, &bytes.Buffer{}, &bytes.Buffer{})
				require.NoError(t, err)
			case cli.ModeRPC:
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				completeProgrammaticRequest(t, fixture, userRequest("catalogs", "inspect catalog"))
				fixture.closeOwner(t)
			}

			// Assert: the external process received neutral descriptors, provider order, selection, and identity.
			require.Equal(t, int32(2), count.Load())
			encoded := catalogueToolOutput(t, body.Load().([]byte))
			var report struct {
				// Identity contains the SDK invocation identity.
				Identity jsontext.Value `json:"identity"`
				// Models contains the complete public model catalog.
				Models jsontext.Value `json:"models"`
				// Providers contains the public provider projection.
				Providers jsontext.Value `json:"providers"`
			}
			require.NoError(t, json.Unmarshal([]byte(encoded), &report))
			identity := new(extensionpb.ExtensionContext)
			models := new(extensionpb.GetModelsResult)
			providers := new(extensionpb.GetProvidersResult)
			require.NoError(t, protojson.Unmarshal(report.Identity, identity))
			require.NoError(t, protojson.Unmarshal(report.Models, models))
			require.NoError(t, protojson.Unmarshal(report.Providers, providers))
			assert.NotEmpty(t, identity.GetContextId())
			assert.NotEmpty(t, identity.GetRuntimeInstanceId())
			assert.NotEmpty(t, identity.GetSessionId())
			assert.NotEmpty(t, identity.GetCwd())
			assert.Equal(t, "external", identity.GetExtensionId())
			require.Len(t, models.GetModels(), 1)
			descriptor := models.GetModels()[0]
			assert.Equal(t, int64(131072), descriptor.GetContextWindow())
			assert.Equal(t, int64(16384), descriptor.GetMaxTokens())
			assert.Equal(
				t,
				[]extensionpb.InputModality{extensionpb.InputModality_INPUT_MODALITY_TEXT},
				descriptor.GetInputModalities(),
			)
			assert.Equal(t, []string{"off"}, descriptor.GetReasoning().GetChoices())
			require.NotNil(t, descriptor.GetTools())
			require.NotNil(t, descriptor.GetPricing())
			assert.Equal(t, "gpt-test", models.GetActiveSelection().GetModelId())
			require.Len(t, providers.GetProviders(), 1)
			assert.Equal(t, "openai-codex", providers.GetProviders()[0].GetProviderId())
			assert.Equal(t, []string{"gpt-test"}, providers.GetProviders()[0].GetModelIds())
			for _, excluded := range []string{"access_token", "refresh", "https://", "enc-restart", "credentials"} {
				assert.NotContains(t, encoded, excluded)
			}
		})
	}
}

// buildPublicCatalogueExtension builds the external module without Host-internal source imports.
func buildPublicCatalogueExtension(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(
		t.Context(),
		"go",
		"build",
		"-o",
		filepath.Join(directory, "external"),
		"./cmd/extension",
	)
	command.Dir = filepath.Join(repoRoot(t), "testdata", "external-plugins")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}

// catalogueProviderTransport requests one catalog tool call, then records its returned model-visible result.
func catalogueProviderTransport(
	t *testing.T,
	count *atomic.Int32,
	body *atomic.Value,
	mode func() string,
) *MockHTTPRoundTripper {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body.Store(data)
		response := finalResponseSSE
		if count.Add(1)%2 == 1 {
			arguments := fmt.Sprintf(`{\"mode\":\"%s\"}`, mode())
			response = strings.NewReplacer(`"bash"`, `"external"`, `{\"command\":\"printf tool-ok\"}`, arguments).
				Replace(toolResponseSSE)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header),
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport
}

// catalogueToolOutput extracts only the external tool result, not provider-owned reasoning from the surrounding
// request.
func catalogueToolOutput(t *testing.T, body []byte) string {
	t.Helper()
	var request struct {
		// Input contains the provider request's ordered history items.
		Input []struct {
			// Type identifies a tool-result item.
			Type string `json:"type"`
			// Output contains model-visible tool result blocks.
			Output []struct {
				// Text contains the exact external extension result.
				Text string `json:"text"`
			} `json:"output"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	var latest string
	for _, item := range request.Input {
		if item.Type == "function_call_output" {
			require.Len(t, item.Output, 1)
			latest = item.Output[0].Text
		}
	}
	require.NotEmpty(t, latest, "provider history has no external tool result")
	return latest
}
