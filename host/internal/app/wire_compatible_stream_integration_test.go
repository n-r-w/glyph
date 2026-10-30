//go:build integration

package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPublicWireCompatibleStreamSecrets protects acquired response credentials through both public compatible APIs.
func TestPublicWireCompatibleStreamSecrets(t *testing.T) {
	t.Parallel()
	// Arrange a real public extension shared by isolated compatible-provider assemblies.
	directory := buildWireFixture(t, "wire")
	for _, api := range []string{"chat-completions", "responses"} {
		t.Run(api, func(t *testing.T) {
			t.Parallel()
			// Arrange a successful response observer followed by a provider error echoing its session credential.
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "B", r.Header.Get("X-Glyph-Wire"))
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Set-Cookie", "session=cookie-private; Path=/; HttpOnly; SameSite=Lax")
				_, _ = io.WriteString(w, "data: "+`{"error":{"message":"`+
					`complete provider failure account=ordinary-account `+
					`Set-Cookie: session=cookie-private; Path=/; HttpOnly; SameSite=Lax",`+
					`"type":"invalid_request_error"}}`+"\n\n")
			}))
			defer server.Close()
			configuration := newCompatibleWireSettings(api, server.URL, "retry:\n  enabled: false\n")
			fixture := startProgrammaticFixtureWithExtension(t, testPaths(t, configuration), directory)
			defer fixture.closeOwner(t)
			// Act through the real Programmatic operation and external response observers.
			failure, _ := requestProgrammaticFailure(t, fixture, userRequest("compatible-stream-secrets", "user input"))
			// Assert normal provider failure, one dispatch and complete safe diagnostic context.
			assert.Equal(t, "MODEL_FAILED", failure.Code)
			assert.Equal(t, 1, calls)
			assert.Contains(t, failure.Message, "complete provider failure account=ordinary-account")
			assert.Contains(t, failure.Message, "Set-Cookie: session=[REDACTED]; Path=/; HttpOnly; SameSite=Lax")
			assert.NotContains(t, failure.Message, "cookie-private")
		})
	}
}
