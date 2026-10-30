//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
)

// TestPublicWireConfiguredAcrossModes shares wire handlers across outer and configured calls in every assembly.
//
//nolint:paralleltest // These scenarios replace the process-global provider HTTP transport.
func TestPublicWireConfiguredAcrossModes(t *testing.T) {
	// Arrange independently built public extension processes sharing only public contracts and SDK.
	directory := buildPublicExtensionFixture(t)
	wireDirectory := buildWireFixture(t, "wire")
	binary, err := os.ReadFile(filepath.Join(wireDirectory, "wire"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "wire"), binary, 0o700))
	for _, mode := range []cli.Mode{cli.ModeHeadless, cli.ModeUI, cli.ModeRPC} {
		t.Run(string(mode), func(t *testing.T) {
			// Arrange a contextual tool that performs one explicit configured-model request.
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := configuredRequestProviderTransport(t, "configured-request", false)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			// Act through the public caller for this application mode.
			runPublicExtensionMode(t, mode, paths, directory, "configured-request", "request configured model")
			// Assert both ordinary attempts and the configured request used the same wire protocol.
			requests := bodies.snapshot()
			require.Len(t, requests, 3)
			for _, raw := range requests {
				var body map[string]any
				require.NoError(t, json.Unmarshal(raw, &body))
				require.Equal(t, "B", body["glyph_wire_marker"])
			}
		})
	}
}
