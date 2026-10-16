//go:build integration

package app

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardManualCompaction persists repeated summaries through the bundled public policy and generator.
func TestStandardManualCompaction(t *testing.T) {
	const standardConversationModelID = "gpt-active"

	// Arrange the real bundled executable, catalog, application, and provider adapter.
	t.Setenv(standardRetainedEnvironment, "1")
	directory := buildStandardCompaction(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("standard-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("standard-recent", manualRetainedText))
	// Act through public Programmatic Control, then repeat after a new conversation turn.
	terminal := requestProgrammaticCompaction(t, fixture, "standard-first", "keep architectural decisions")
	require.NotNil(t, terminal.GetCompleted(), "failure: %s", terminal.GetFailed().GetMessage())
	marker := terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction()
	require.NotEmpty(t, marker.GetSummary())
	require.Len(t, marker.GetSource().GetModelRequests().GetRequests(), 1)
	require.Equal(t, standardConversationModelID, marker.GetSource().GetModelRequests().GetRequests()[0].GetModelId())
	completeProgrammaticRequest(t, fixture, userRequest("standard-next", manualNextText))
	second := requestProgrammaticCompaction(t, fixture, "standard-second", "keep pending work")
	require.NotNil(t, second.GetCompleted(), "failure: %s", second.GetFailed().GetMessage())
	stored := sendProgrammaticOperation(t, fixture, "standard-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	// Assert originals, one marker per operation, one generation per compaction, and preceding-summary input.
	originals, markers := countManualStoredEntries(stored)
	require.Equal(t, 1, originals)
	require.Equal(t, 2, markers)
	actual := bodies.snapshot()
	require.Len(t, actual, 5)
	require.Contains(t, string(actual[2]), manualOriginalText)
	require.Contains(t, string(actual[3]), marker.GetSummary())
	require.NotContains(t, string(actual[3]), manualOriginalText)
	require.Contains(t, string(actual[4]), marker.GetSummary())
	require.NotContains(t, string(actual[4]), manualOriginalText)
	fixture.closeOwner(t)
}

// TestStandardManualCompactionRejectsUnavailableWork checks rejection of unavailable compaction work.
func TestStandardManualCompactionRejectsUnavailableWork(t *testing.T) {
	for _, test := range []struct {
		name     string
		retained string
		enabled  string
		cause    string
	}{
		{name: "retained-history", retained: "20000", enabled: "true", cause: "no older reducible"},
		{name: "disabled-generator", retained: "1", enabled: "false", cause: "exactly one generator"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange the catalog with independently configured policy and generator.
			t.Setenv(standardRetainedEnvironment, test.retained)
			t.Setenv(standardGeneratorEnvironment, test.enabled)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildStandardCompaction(t))
			completeProgrammaticRequest(t, fixture, userRequest("standard-request", manualOriginalText))
			// Act through the public manual operation.
			terminal := requestProgrammaticCompaction(t, fixture, "standard-rejected", "guidance")
			// Assert the complete cause, no provider generation, and no stored marker.
			require.NotNil(t, terminal.GetFailed())
			require.Contains(t, terminal.GetFailed().GetMessage(), test.cause)
			require.Len(t, bodies.snapshot(), 1)
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"standard-rejected-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			_, markers := countManualStoredEntries(stored)
			require.Zero(t, markers)
			fixture.closeOwner(t)
		})
	}
}

// TestStandardUIManualCompaction runs the bundled extension through the real UI SDK operation.
func TestStandardUIManualCompaction(t *testing.T) {
	// Arrange the bundled catalog and existing public UI client fixture.
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	t.Setenv(standardRetainedEnvironment, "1")
	directory := buildStandardCompaction(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	uiDirectory := t.TempDir()
	reportPath := filepath.Join(t.TempDir(), "standard-ui.json")
	t.Setenv(appUITraceEnvironment, reportPath)
	t.Setenv(manualGuidanceEnvironment, "preserve decisions")
	writeUIExecutable(t, uiDirectory, manualFixtureManualUi)
	// Act through the public UI connection.
	err := runWithPaths(
		t.Context(),
		paths,
		cli.Command{
			Mode:               cli.ModeUI,
			Headless:           headless.Command{UserText: "", ExtensionDirectory: directory},
			ExtensionDirectory: directory,
			UIDirectory:        uiDirectory,
			UIID:               manualFixtureManualUi,
			SocketPath:         "",
		},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	// Assert one durable replacement and a successful conversation after the required acknowledgement callback.
	require.NoError(t, err)
	encoded, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report manualPublicReport
	require.NoError(t, json.Unmarshal(encoded, &report))
	require.True(t, report.Committed, report.Error)
	require.Empty(t, report.Error)
	require.Equal(t, 1, report.Markers)
	require.Equal(t, 1, report.Originals)
	require.Len(t, bodies.snapshot(), 4)
}

// buildStandardCompaction builds the production executable into an ordinary extension catalog.
func buildStandardCompaction(t *testing.T) string {
	const standardCompactionExecutable = "glyph-compaction"

	const (
		goCommand         = "go"
		buildCommand      = "build"
		outputFlag        = "-o"
		compactionPackage = "./plugins/extension/compaction/cmd/glyph-compaction"
	)
	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(
		t.Context(),
		goCommand,
		buildCommand,
		outputFlag,
		filepath.Join(directory, standardCompactionExecutable),
		compactionPackage,
	)
	command.Dir = repoRoot(t)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}
