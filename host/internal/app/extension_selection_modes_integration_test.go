//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// externalSelectionObserversEnvironment enables public selection lifecycle observers.
	externalSelectionObserversEnvironment = "GLYPH_EXTERNAL_SELECTION_OBSERVERS"
	// reasoningSelectionObservedSignal identifies one completed public reasoning observer.
	reasoningSelectionObservedSignal = "reasoning-selection-observed"
	// externalSelectionObserverErrorEnvironment makes the first reasoning observer fail ordinarily.
	externalSelectionObserverErrorEnvironment = "GLYPH_EXTERNAL_SELECTION_OBSERVER_ERROR"
	// laterReasoningSelectionObservedSignal identifies continuation after an ordinary observer error.
	laterReasoningSelectionObservedSignal = "reasoning-selection-later-observed"
)

// TestPublicExtensionSelectionAcrossApplicationModes verifies real process selection in every Host assembly.
func TestPublicExtensionSelectionAcrossApplicationModes(t *testing.T) {
	// Arrange one public-only extension binary shared by each isolated application scenario.
	directory := buildPublicExtensionFixture(t)
	for _, scenario := range []struct {
		// name identifies the application assembly.
		name string
		// mode selects the connected Glyph client contract.
		mode cli.Mode
	}{
		{name: "headless", mode: cli.ModeHeadless},
		{name: "ui", mode: cli.ModeUI},
		{name: "programmatic", mode: cli.ModeRPC},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv(externalSelectionObserversEnvironment, "1")
			signals := t.TempDir()
			t.Setenv(externalSignalsEnvironment, signals)
			paths := testPaths(t, selectionCompositionSettings())
			writeProgrammaticCredentials(t, paths)
			transport, bodies := extensionSelectionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })

			// Act: invoke the real extension tool, which selects model and reasoning over ExtensionService.Open.
			runPublicExtensionMode(t, scenario.mode, paths, directory, "selection", "select active model")

			// Assert: the tool returned the committed full selection and ordered empty diagnostics.
			actualBodies := bodies.snapshot()
			require.Len(t, actualBodies, 2)
			encoded := externalToolOutput(t, actualBodies[1])
			result := new(extensionpb.SelectionResult)
			require.NoError(t, protojson.Unmarshal([]byte(encoded), result))
			assert.Equal(t, "openai-codex", result.GetSelection().GetProviderId())
			assert.Equal(t, "gpt-test", result.GetSelection().GetModelId())
			assert.Equal(t, "high", result.GetSelection().GetReasoningChoice())
			assert.Empty(t, result.GetIssues())
			_, err := os.Stat(signals + "/" + reasoningSelectionObservedSignal)
			require.NoError(t, err)
		})
	}
}

// TestPublicSelectionObserverErrorCompletesWithDiagnostics verifies ordinary failures and continuation publicly.
func TestPublicSelectionObserverErrorCompletesWithDiagnostics(t *testing.T) {
	// Arrange two real reasoning observers where the first fails ordinarily.
	t.Setenv(externalSelectionObserversEnvironment, "1")
	t.Setenv(externalSelectionObserverErrorEnvironment, "1")
	signals := t.TempDir()
	t.Setenv(externalSignalsEnvironment, signals)
	paths := testPaths(t, selectionCompositionSettings())
	writeProgrammaticCredentials(t, paths)
	transport, bodies := extensionSelectionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through the public extension tool and real process observer dispatch.
	runPublicExtensionMode(
		t, cli.ModeRPC, paths, buildPublicExtensionFixture(t), "selection", "select active model",
	)

	// Assert committed state, typed observer diagnostics, later observation, and continued extension execution.
	actualBodies := bodies.snapshot()
	require.Len(t, actualBodies, 2)
	result := new(extensionpb.SelectionResult)
	require.NoError(t, protojson.Unmarshal([]byte(externalToolOutput(t, actualBodies[1])), result))
	require.Len(t, result.GetIssues(), 1)
	assert.Equal(
		t, extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_OBSERVER_ERROR, result.GetIssues()[0].GetCode(),
	)
	assert.Contains(t, result.GetIssues()[0].GetMessage(), "public reasoning observer failed")
	_, err := os.Stat(signals + "/" + laterReasoningSelectionObservedSignal)
	require.NoError(t, err)
}

// extensionSelectionProviderTransport returns one tool call followed by one outer completion.
func extensionSelectionProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := finalResponseSSE
		if position == 1 {
			response = strings.NewReplacer(
				`"bash"`, `"external"`,
				`{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"selection\"}`,
			).Replace(toolResponseSSE)
		} else if position != 2 {
			t.Fatalf("unexpected provider request %d", position)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header),
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}
