//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestAutomaticFinalCheckFreezesOutgoingHistory checks that an append during final validation
// belongs to the next request.
func TestAutomaticFinalCheckFreezesOutgoingHistory(t *testing.T) {
	// Arrange report paths and a final-check append before starting the connected reference owner.
	const lateText = "later final validation append"
	scratch := t.TempDir()
	outgoingPath := filepath.Join(scratch, "outgoing.json")
	t.Setenv(automaticFixtureModeEnvironment, automaticFixtureAppendFinalMode)
	t.Setenv(automaticFixtureTargetEnvironment, automaticFixtureTarget)
	t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
	t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("freeze-seed", manualOriginalText))
	// Act through replacement, final-validation growth and a later ordinary conversation request.
	completeProgrammaticRequest(t, fixture, userRequest("freeze-next", automaticFixtureTarget))
	completeProgrammaticRequest(t, fixture, userRequest("freeze-later", manualNextText))
	// Assert the dispatched replacement equals the earlier final capture while the next request includes later growth.
	encoded, err := os.ReadFile(outgoingPath)
	require.NoError(t, err)
	require.Contains(t, string(encoded), automaticFixtureObserverText)
	require.NotContains(t, string(encoded), lateText)
	actual := bodies.snapshot()
	require.Len(t, actual, 3)
	require.Contains(t, string(actual[1]), automaticFixtureSummary)
	require.Contains(t, string(actual[1]), automaticFixtureObserverText)
	require.NotContains(t, string(actual[1]), lateText)
	require.Contains(t, string(actual[2]), lateText)
	stored := sendProgrammaticOperation(t, fixture, "freeze-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	originals, markers := countManualStoredEntries(stored)
	require.Equal(t, 1, originals)
	require.Equal(t, 1, markers)
}
