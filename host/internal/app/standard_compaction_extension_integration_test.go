//go:build integration

package app

import (
	"encoding/json/v2"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestStandardExtensionManualCompaction uses an independently running public Extension SDK caller.
func TestStandardExtensionManualCompaction(t *testing.T) {
	const standardCallerExecutable = "reference-caller"

	// Arrange the bundled policy and a caller-only reference runtime.
	t.Setenv(standardRetainedEnvironment, "1")
	t.Setenv("GLYPH_REFERENCE_POLICY_DISABLED", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_OBSERVER", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_REQUESTS", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_GENERATOR", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_RESULTS", "1")
	socket := filepath.Join(t.TempDir(), "standard-caller.sock")
	t.Setenv(manualFixtureGlyphReferenceCallerSocket, socket)
	directory := buildStandardCompaction(t)
	fixtureDirectory := buildManualPolicyFixture(t)
	binary, err := os.ReadFile(filepath.Join(fixtureDirectory, manualFixtureReferencePolicy))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, standardCallerExecutable), binary, 0o700))
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("standard-extension-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("standard-extension-recent", manualRetainedText))
	// Act by signaling the caller after conversation settlement.
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socket, Net: "unix"})
	require.NoError(t, err)
	require.NoError(t, connection.SetDeadline(time.Now().Add(15*time.Second)))
	_, err = connection.Write([]byte("preserve decisions"))
	require.NoError(t, err)
	require.NoError(t, connection.CloseWrite())
	encoded, err := io.ReadAll(connection)
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	// Assert the caller receives a durable receipt and the next provider request contains the committed summary.
	var report manualPublicReport
	require.NoError(t, json.Unmarshal(encoded, &report))
	require.True(t, report.Committed, report.Error)
	require.Empty(t, report.Error)
	completeProgrammaticRequest(t, fixture, userRequest("standard-extension-next", manualNextText))
	actual := bodies.snapshot()
	require.Len(t, actual, 4)
	require.Contains(t, string(actual[3]), report.Summary)
	require.NotContains(t, string(actual[3]), manualOriginalText)
	fixture.closeOwner(t)
}
