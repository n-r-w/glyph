//go:build integration

package app

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// buildWireFixture builds the public-only wire reference with the selected executable scenario.
func buildWireFixture(t *testing.T, mode string) string {
	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(directory, mode), "./cmd/wire")
	command.Dir = filepath.Join(repoRoot(t), "testdata", "external-plugins")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}

// newWireProviderFixture routes Codex requests to a recording provider that checks the final wire header.
func newWireProviderFixture(t *testing.T, failures int) *requestBodies {
	t.Helper()
	return newRecordingProviderFixture(t, failures, func(r *http.Request) {
		assert.Equal(t, "B", r.Header.Get("X-Glyph-Wire"))
	})
}

// newRecordingProviderFixture routes Codex requests to a recording local server,
// which answers the first failures requests with HTTP 429 and checks each request with inspect when supplied.
func newRecordingProviderFixture(t *testing.T, failures int, inspect func(*http.Request)) *requestBodies {
	t.Helper()
	bodies := new(requestBodies)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		position := bodies.append(body)
		if inspect != nil {
			inspect(r)
		}
		if position <= failures {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"ordinary provider rate limit"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, finalResponseSSE)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	previous := http.DefaultTransport
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(r *http.Request) (*http.Response, error) {
		cloned := r.Clone(r.Context())
		cloned.URL.Scheme = target.Scheme
		cloned.URL.Host = target.Host
		return previous.RoundTrip(cloned)
	}).AnyTimes()
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	return bodies
}

// TestPublicWireMiddleware composes wire values through ordinary public extension operations.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicWireMiddleware(t *testing.T) {
	// Arrange the real public wire extension, Host assembly and local HTTP provider.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	bodies := newWireProviderFixture(t, 0)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildWireFixture(t, "wire"))
	defer fixture.closeOwner(t)
	// Act through a public accepted user operation.
	completeProgrammaticRequest(t, fixture, userRequest("wire-success", "user input"))
	// Assert the final replacement reached HTTP while durable history stayed request-local-marker-free.
	received := bodies.snapshot()
	require.Len(t, received, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal(received[0], &body))
	require.Equal(t, "B", body["glyph_wire_marker"])
	history := sendProgrammaticOperation(t, fixture, "wire-history", func(r *programmaticv1.OpenRequest) {
		programmaticRequest(r).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	require.Len(t, history, 2)
	require.Equal(t, "user input", history[0].GetUser().GetContent()[0].GetText())
	require.Equal(t, "Request complete.", history[1].GetModel().GetText())
}

// TestPublicWireFailure exposes complete ordinary failures and prevents dispatch or recovery retries.
//
//nolint:paralleltest // These scenarios replace the process-global provider HTTP transport.
func TestPublicWireFailure(t *testing.T) {
	for _, scenario := range []struct {
		mode  string
		calls int
		cause string
	}{
		{mode: "wire-fail-headers", calls: 0, cause: "reference wire handler failed: unexpected EOF"},
		{mode: "wire-fail-body", calls: 0, cause: "reference wire handler failed: unexpected EOF"},
		{mode: "wire-invalid", calls: 0, cause: "invalid body action"},
		{mode: "wire-bad-reference", calls: 0, cause: "unknown private replay reference"},
		{mode: "wire-fail-response", calls: 1, cause: "reference wire handler failed: unexpected EOF"},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			// Arrange the assembled public path with retry enabled and a failing ordinary extension.
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			bodies := newWireProviderFixture(t, scenario.calls)
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildWireFixture(t, scenario.mode))
			defer fixture.closeOwner(t)
			// Act through the actual client operation and wait for terminal failure.
			failure, _ := requestProgrammaticFailure(t, fixture, userRequest("wire-failure", "user input"))
			// Assert the public category and full cause without provider recovery.
			require.Equal(t, "EXTENSION_FAILED", failure.Code)
			require.Contains(t, failure.Message, scenario.cause)
			require.Len(t, bodies.snapshot(), scenario.calls)
		})
	}
}
