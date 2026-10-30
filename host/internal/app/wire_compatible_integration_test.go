//go:build integration

package app

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicWireCompatibleFamilies invokes real public wire handlers through Chat and Responses provider assemblies.
func TestPublicWireCompatibleFamilies(t *testing.T) {
	t.Parallel()
	// Arrange a public-only reference executable shared by isolated application scenarios.
	directory := buildWireFixture(t, "wire")
	for _, api := range []string{"chat-completions", "responses"} {
		t.Run(api, func(t *testing.T) {
			t.Parallel()
			// Arrange a real provider endpoint and compatible-provider settings.
			bodies := new(requestBodies)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				bodies.append(body)
				assert.Equal(t, "B", r.Header.Get("X-Glyph-Wire"))
				w.Header().Set("Content-Type", "text/event-stream")
				response := finalResponseSSE
				if api == "chat-completions" {
					response = "data: " + `{"id":"chat","model":"gpt-test","choices":[` +
						`{"index":0,"delta":{"content":"Request complete."},"finish_reason":"stop"}]}` +
						"\n\ndata: [DONE]\n\n"
				}
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()
			configuration := newCompatibleWireSettings(api, server.URL, "")
			paths := testPaths(t, configuration)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			// Act through an actual Programmatic operation and external extension process.
			completeProgrammaticRequest(t, fixture, userRequest("compatible-wire", "user input"))
			// Assert public immutable-original replacement reached each provider serializer.
			received := bodies.snapshot()
			require.Len(t, received, 1)
			var request map[string]any
			require.NoError(t, json.Unmarshal(received[0], &request))
			require.Equal(t, "B", request["glyph_wire_marker"])
		})
	}
}
