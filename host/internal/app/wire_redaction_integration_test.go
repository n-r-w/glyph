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

// TestPublicWireAcquiredRedaction preserves complete public failure diagnostics without exposing the request token.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicWireAcquiredRedaction(t *testing.T) {
	// Arrange real Host, a public extension process and an echoing provider rejection.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	token := semanticAccessToken(t, "account")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"complete provider detail token=%s account=%s"}}`,
			token, r.Header.Get("chatgpt-account-id"))
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
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildWireFixture(t, "wire-fail-response"))
	defer fixture.closeOwner(t)
	// Act through a public Programmatic operation.
	failure, _ := requestProgrammaticFailure(t, fixture, userRequest("wire-public-redaction", "user input"))
	// Assert the category, complete non-secret causes, account and status remain after one request.
	require.Equal(t, "EXTENSION_FAILED", failure.Code)
	require.Equal(t, 1, calls)
	assert.Contains(t, failure.Message, "reference wire handler failed: unexpected EOF")
	assert.Contains(t, failure.Message, "complete provider detail token=[REDACTED] account=account")
	assert.Contains(t, failure.Message, "HTTP 429 Too Many Requests")
	assert.NotContains(t, failure.Message, token)
}
