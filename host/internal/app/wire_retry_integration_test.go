//go:build integration

package app

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPublicWireNormalProviderRetry retries ordinary provider failures after successful public response observation.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicWireNormalProviderRetry(t *testing.T) {
	// Arrange a real extension and a provider returning 429 once before a successful response.
	paths := testPaths(t, codexSettings("retry:\n  enabled: true\n  maxRetries: 1\n  delays: [0s]\n"))
	writeProgrammaticCredentials(t, paths)
	bodies := newWireProviderFixture(t, 1)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildWireFixture(t, "wire"))
	defer fixture.closeOwner(t)
	// Act through the ordinary public request and Host-owned retry policy.
	completeProgrammaticRequest(t, fixture, userRequest("wire-retry", "user input"))
	// Assert each genuine attempt used a fresh ordered wire chain and normal retry remained admitted.
	received := bodies.snapshot()
	require.Len(t, received, 2)
	for _, raw := range received {
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		require.Equal(t, "B", body["glyph_wire_marker"])
	}
}
