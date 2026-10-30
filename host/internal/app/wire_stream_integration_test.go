//go:build integration

package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestPublicWireStreamSecrets protects acquired response credentials in ordinary public provider failures.
//
//nolint:paralleltest // This fixture replaces the process-global provider transport.
func TestPublicWireStreamSecrets(t *testing.T) {
	// Arrange a real public extension and successful HTTP metadata followed by a provider SSE error.
	paths := testPaths(t, codexSettings("retry:\n  enabled: false\n"))
	writeProgrammaticCredentials(t, paths)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "session=cookie-private; Path=/; HttpOnly; SameSite=Lax")
		_, _ = fmt.Fprint(
			w,
			"data: "+`{"type":"error","code":"invalid_request_error","message":"`+
				`complete provider stream failure account=account `+
				`Set-Cookie: session=cookie-private; Path=/; HttpOnly; SameSite=Lax"}`+"\n\n",
		)
	}))
	defer server.Close()
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
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildWireFixture(t, "wire"))
	defer fixture.closeOwner(t)
	// Act through a real Programmatic operation with provider retries disabled.
	failure, _ := requestProgrammaticFailure(t, fixture, userRequest("wire-stream-secrets", "user input"))
	// Assert normal provider category, one request and full non-secret diagnostic context.
	assert.Equal(t, "MODEL_FAILED", failure.Code)
	assert.Equal(t, 1, calls)
	assert.Contains(t, failure.Message, "complete provider stream failure account=account")
	assert.Contains(t, failure.Message, "Set-Cookie: session=[REDACTED]; Path=/; HttpOnly; SameSite=Lax")
	assert.NotContains(t, failure.Message, "cookie-private")
}
