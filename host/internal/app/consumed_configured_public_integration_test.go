//go:build integration

package app

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	// consumedConfiguredFixtureMode selects the ordinary tool that catches a retry-handler failure.
	consumedConfiguredFixtureMode = "consumed-configured-failure"
	// consumedConfiguredFixtureReport identifies the plugin's public SDK outcome report.
	consumedConfiguredFixtureReport = "consumed-configured-failure.json"
	// consumedRetryFailureText is the complete existing retry-handler cause.
	consumedRetryFailureText = "retry handler full unique failure text"
)

// TestPublicConsumedExtensionFailureFailsParent verifies the actual failed terminal and later independent usability.
func TestPublicConsumedExtensionFailureFailsParent(t *testing.T) {
	// Arrange a real plugin, a configured child transient response and an existing failing retry handler.
	signals := t.TempDir()
	t.Setenv(externalSignalsEnvironment, signals)
	t.Setenv("GLYPH_EXTERNAL_RETRY_HANDLER", "fail")
	paths := testPaths(t, retryModeSettings)
	writeProgrammaticCredentials(t, paths)
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		payload := finalResponseSSE
		switch position {
		case 1:
			payload = consumedConfiguredToolResponse(consumedConfiguredFixtureMode)
		case 2:
			payload = retryPartialFailureSSE
		case 3:
			payload = consumedConfiguredToolResponse("ordinary")
		case 4:
		default:
			t.Fatalf("unexpected provider request %d", position)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).Times(4)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))

	// Act through Programmatic Control while the plugin catches its child's returned extension failure.
	failure, retryProgress := requestProgrammaticFailure(
		t,
		fixture,
		userRequest("consumed-public", "invoke consumed child"),
	)

	// Assert one failed parent terminal preserves both extension and provider causes without another request.
	require.Equal(t, "EXTENSION_FAILED", failure.Code)
	require.Contains(t, failure.Message, consumedRetryFailureText)
	require.Contains(t, failure.Message, "temporary provider failure")
	require.Zero(t, retryProgress)
	require.Len(t, bodies.snapshot(), 2)
	encoded, err := os.ReadFile(filepath.Join(signals, consumedConfiguredFixtureReport))
	require.NoError(t, err)
	var report struct {
		Failure string `json:"failure"`
		Later   string `json:"later"`
	}
	require.NoError(t, json.Unmarshal(encoded, &report))
	require.Contains(t, report.Failure, consumedRetryFailureText)
	require.Contains(t, report.Later, "parent invocation")
	// Assert the settled owner accepts a separate run and its ordinary tool on the same extension connection.
	completeProgrammaticRequest(t, fixture, userRequest("independent-public", "invoke ordinary tool"))
	require.Len(t, bodies.snapshot(), 4)
	fixture.closeOwner(t)
}

// consumedConfiguredToolResponse selects a public fixture tool mode in the provider response.
func consumedConfiguredToolResponse(mode string) string {
	const argumentsTemplate = `{\"mode\":\"%s\"}`
	return strings.NewReplacer(
		`"bash"`, `"external"`,
		`{\"command\":\"printf tool-ok\"}`, strings.Replace(argumentsTemplate, "%s", mode, 1),
	).Replace(toolResponseSSE)
}
