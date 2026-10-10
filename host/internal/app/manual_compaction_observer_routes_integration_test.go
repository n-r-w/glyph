//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

// TestScenario checks the actual UI cancellation command after commit.
func (s *uIManualCancellationStopsObserverModelSuite) TestScenario() {
	t := s.T()
	for _, phase := range []struct {
		// guidance selects the pending policy or observer model phase.
		guidance string
		// committed reports whether persistence precedes cancellation.
		committed bool
	}{
		{guidance: manualFixtureObserveCancellation, committed: true},
		{guidance: manualFixtureWaitForCallerCancel, committed: false},
	} {
		t.Run(phase.guidance, func(t *testing.T) {
			// Arrange a real UI caller with pending observer provider execution.

			t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
			t.Setenv(manualGuidanceEnvironment, phase.guidance)
			socket := filepath.Join(t.TempDir(), manualFixtureCancelSock)
			t.Setenv(manualFixtureGlyphManualCancelSocket, socket)
			reportPath := filepath.Join(t.TempDir(), manualFixtureReportJson)
			t.Setenv(appUITraceEnvironment, reportPath)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			directory := buildManualPolicyFixture(t)
			uiDirectory := t.TempDir()
			writeUIExecutable(t, uiDirectory, manualFixtureManualUi)
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			previous := http.DefaultTransport
			http.DefaultTransport = newObserverCancellationTransport(t, started, stopped, release)
			t.Cleanup(func() { http.DefaultTransport = previous })
			done := make(chan error, 1)
			go func() {
				done <- runWithPaths(t.Context(), paths, cli.Command{
					Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: directory},
					ExtensionDirectory: directory, UIDirectory: uiDirectory, UIID: manualFixtureManualUi, SocketPath: "",
				}, &bytes.Buffer{}, &bytes.Buffer{})
			}()

			// Act through a public UI Cancel request while observer model execution is pending.
			<-started
			signalObserverCancellation(t, socket, stopped)
			require.NoError(t, <-done)

			// Assert joined termination with the durable receipt and original stored entries.
			encoded, err := os.ReadFile(reportPath)
			require.NoError(t, err)
			var report manualPublicReport
			require.NoError(t, json.Unmarshal(encoded, &report))
			if phase.committed {
				assertCanceledObserverReceipt(t, report)
			} else {
				require.False(t, report.Committed, report.Error)
				require.Contains(t, report.Error, "context canceled")
			}
			if phase.committed {
				require.Equal(t, 1, report.Markers)
			} else {
				require.Zero(t, report.Markers)
			}
			require.Equal(t, 1, report.Originals)
		})
	}
}

// TestScenario checks public Extension owner cancellation after commit.
func (s *extensionManualCancellationStopsObserverModelSuite) TestScenario() {
	t := s.T()
	for _, phase := range []struct {
		// guidance selects the pending policy or observer model phase.
		guidance string
		// committed reports whether persistence precedes cancellation.
		committed bool
	}{
		{guidance: manualFixtureObserveCancellation, committed: true},
		{guidance: manualFixtureWaitForCallerCancel, committed: false},
	} {
		t.Run(phase.guidance, func(t *testing.T) {
			// Arrange an ordinary SDK caller and observer-owned provider work.

			callerSocket := filepath.Join(t.TempDir(), manualFixtureCallerSock)
			cancelSocket := filepath.Join(t.TempDir(), manualFixtureCancelSock)
			t.Setenv(manualFixtureGlyphReferenceCallerSocket, callerSocket)
			t.Setenv(manualFixtureGlyphManualCancelSocket, cancelSocket)
			directory := buildManualPolicyFixture(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			previous := http.DefaultTransport
			http.DefaultTransport = newObserverCancellationTransport(t, started, stopped, release)
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))
			connection, err := net.DialUnix(
				manualFixtureUnix,
				nil,
				&net.UnixAddr{Name: callerSocket, Net: manualFixtureUnix},
			)
			require.NoError(t, err)
			require.NoError(t, connection.SetDeadline(time.Now().Add(15*time.Second)))
			_, err = connection.Write([]byte(phase.guidance))
			require.NoError(t, err)
			require.NoError(t, connection.CloseWrite())

			// Act through the SDK operation owner's cancellation signal.
			<-started
			signalObserverCancellation(t, cancelSocket, stopped)
			encoded, err := io.ReadAll(connection)
			require.NoError(t, err)
			require.NoError(t, connection.Close())

			// Assert joined termination and independent receipt delivery to the Extension caller.
			var report manualPublicReport
			require.NoError(t, json.Unmarshal(encoded, &report))
			if phase.committed {
				assertCanceledObserverReceipt(t, report)
			} else {
				require.False(t, report.Committed, report.Error)
				require.Contains(t, report.Error, "context canceled")
			}
			fixture.closeOwner(t)
		})
	}
}

// signalObserverCancellation checks cancellation propagation before releasing provider cleanup.
func signalObserverCancellation(t *testing.T, socket string, stopped <-chan struct{}) {
	t.Helper()
	connection, err := net.DialUnix(manualFixtureUnix, nil, &net.UnixAddr{Name: socket, Net: manualFixtureUnix})
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "owning cancellation did not stop observer provider work")
	}
}

// assertCanceledObserverReceipt checks complete cancellation diagnostics after durable persistence.
func assertCanceledObserverReceipt(t *testing.T, report manualPublicReport) {
	t.Helper()
	require.True(t, report.Committed, report.Error)
	require.Equal(t, manualReferenceSummary, report.Summary)
	require.Contains(t, report.Error, "context canceled")
}

// uiCancellationFailureSuite owns the cancellation SDK failure observation.
type uiCancellationFailureSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUICancellationFixtureRetainsRequestFailure checks an acquired SDK cancellation failure.
func TestUICancellationFixtureRetainsRequestFailure(t *testing.T) {
	// Arrange the public fixture setting.
	t.Setenv(manualFixtureGlyphManualCancelFailure, manualFixtureEnabled)
	// Act by running the public scenario.
	suite.Run(t, &uiCancellationFailureSuite{Suite: suite.Suite{}})
	// Assert the public outcome in the suite scenario.
}

// TestScenario checks cancellation failure collection through the real UI fixture.
func (s *uiCancellationFailureSuite) TestScenario() {
	t := s.T()
	// Arrange a live SDK connection with a canceled cancellation-request context.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	t.Setenv(manualGuidanceEnvironment, manualFixtureObserveCancellation)
	socket := filepath.Join(t.TempDir(), manualFixtureCancelSock)
	t.Setenv(manualFixtureGlyphManualCancelSocket, socket)
	reportPath := filepath.Join(t.TempDir(), manualFixtureReportJson)
	t.Setenv(appUITraceEnvironment, reportPath)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	directory := buildManualPolicyFixture(t)
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, manualFixtureManualUi)
	started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	previous := http.DefaultTransport
	http.DefaultTransport = newObserverCancellationTransport(t, started, stopped, release)
	t.Cleanup(func() { http.DefaultTransport = previous })
	done := make(chan error, 1)
	go func() {
		done <- runWithPaths(t.Context(), paths, cli.Command{
			Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: directory},
			ExtensionDirectory: directory, UIDirectory: uiDirectory, UIID: manualFixtureManualUi, SocketPath: "",
		}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	// Act by signaling cancellation, then releasing independently pending observer work.
	<-started
	connection, err := net.DialUnix(manualFixtureUnix, nil, &net.UnixAddr{Name: socket, Net: manualFixtureUnix})
	require.NoError(t, err)
	_, err = io.ReadAll(connection)
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	close(release)
	require.NoError(t, <-done)
	// Assert the fixture owner receives the complete original SDK cancellation cause and durable receipt.
	encoded, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report manualPublicReport
	require.NoError(t, json.Unmarshal(encoded, &report))
	require.True(t, report.Committed)
	require.Contains(t, report.Error, "original UI cancellation request cause")
}
