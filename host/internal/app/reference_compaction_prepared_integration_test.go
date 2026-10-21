//go:build integration

package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReferenceCompactionKeepsLatestOriginalUser preserves boundary selection across rendered user-role records.
func TestReferenceCompactionKeepsLatestOriginalUser(t *testing.T) {
	// Arrange a reference policy whose success observer appends user-role extension context.
	t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, _ := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildManualPolicyFixture(t))
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("origin-first", manualOriginalText))
	first := requestProgrammaticCompaction(t, fixture, "origin-first-compaction", "")
	require.NotNil(t, first.GetCompleted(), "%v", first)
	completeProgrammaticRequest(t, fixture, userRequest("origin-latest", manualNextText))
	entries := preparedStoredEntries(t, fixture)
	latest := ""
	for _, entry := range entries {
		for _, block := range entry.GetUser().GetContent() {
			if block.GetText() == manualNextText {
				latest = entry.GetId()
			}
		}
	}
	require.NotEmpty(t, latest)
	// Act through repeated public compaction, including an invocation with only observer growth after the previous marker.
	for _, operation := range []string{"origin-second-compaction", "origin-third-compaction"} {
		terminal := requestProgrammaticCompaction(t, fixture, operation, "")
		// Assert newer original users replace the old boundary, while summaries and extension messages keep their origins.
		require.NotNil(t, terminal.GetCompleted(), "%v", terminal)
		require.Equal(
			t,
			latest,
			terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction().GetFirstKeptEntryId(),
		)
	}
}
