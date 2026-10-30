//go:build !integration

package openai

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWireCookieDiagnostics preserves cookie names and attributes while masking only credential values.
func TestWireCookieDiagnostics(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		header string
		value  string
		want   string
	}{
		{
			name: "response", header: "Set-Cookie",
			value: "session=cookie-private; Path=/; HttpOnly; SameSite=Lax",
			want:  "session=[REDACTED]; Path=/; HttpOnly; SameSite=Lax",
		},
		{
			name: "request", header: "Cookie", value: "session=cookie-private; refresh=refresh-private",
			want: "session=[REDACTED]; refresh=[REDACTED]",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a complete header echoed in an otherwise ordinary provider diagnostic.
			headers := http.Header{scenario.header: []string{scenario.value}}
			prefix := "HTTP 429 account=ordinary-account " + scenario.header + ": "
			// Act through the production secret collector and diagnostic string masker.
			actual := RedactWireText(prefix+scenario.value, CollectWireSecrets(headers))
			// Assert names, status, ordinary identifiers and all non-secret attributes remain intact.
			assert.Equal(t, prefix+scenario.want, actual)
		})
	}
}
