//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestScenario checks session-bound public Extension manual operations.
func (s *extensionManualCompactionTerminalStatesSuite) TestScenario() {
	const manualFixtureExtensionOriginals = "extension-originals"

	t := s.T()
	// Arrange explicit fixture coordination after conversation settlement.

	for _, scenario := range append([]manualTerminalScenario{
		{name: "success", guidance: manualFixturePreserveTheRecentUserTurn, cause: "", committed: true, canceled: false},
		newManualAbsentOwnerScenario(),
	}, newManualTerminalScenarios()...) {
		t.Run(scenario.guidance, func(t *testing.T) {
			// Arrange the policy and caller fixtures for this terminal result.
			outcomePath := configureManualOutcomeFixture(t, scenario.guidance)
			socket := filepath.Join(t.TempDir(), manualFixtureCallerSock)
			t.Setenv(manualFixtureGlyphReferenceCallerSocket, socket)
			if scenario.guidance == manualNoOwnerGuidance {
				t.Setenv(manualFixtureGlyphReferencePolicyDisabled, manualFixtureEnabled)
			}
			directory := buildManualOutcomePolicyFixture(t, scenario.guidance)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))

			// Act by signaling the actual extension process after the public conversation terminal.
			connection, err := net.DialUnix(manualFixtureUnix, nil, &net.UnixAddr{Name: socket, Net: manualFixtureUnix})
			require.NoError(t, err)
			require.NoError(t, connection.SetDeadline(time.Now().Add(15*time.Second)))
			_, err = connection.Write([]byte(scenario.guidance))
			require.NoError(t, err)
			require.NoError(t, connection.CloseWrite())
			encoded, err := io.ReadAll(connection)
			require.NoError(t, err)
			require.NoError(t, connection.Close())
			var report manualPublicReport
			require.NoError(t, json.Unmarshal(encoded, &report))

			// Assert complete public outcomes, original storage, and next-request projection.
			require.Equal(t, scenario.committed, report.Committed, report.Error)
			require.Equal(t, scenario.canceled, report.Canceled, report.Error)
			if scenario.cause != "" {
				require.Contains(t, report.Error, scenario.cause)
			} else {
				require.Empty(t, report.Error)
			}
			assertManualOutcome(t, outcomePath, scenario.guidance, scenario.committed, report.Error)
			stored := sendProgrammaticOperation(
				t,
				fixture,
				manualFixtureExtensionOriginals,
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			originals, markers := countManualStoredEntries(stored)
			require.Equal(t, 1, originals)
			if scenario.committed {
				require.Equal(t, 1, markers)
				completeProgrammaticRequest(t, fixture, userRequest(manualFixtureNext, manualNextText))
				actual := bodies.snapshot()
				require.Contains(t, string(actual[len(actual)-1]), manualReferenceSummary)
				require.Contains(t, string(actual[len(actual)-1]), manualRetainedText)
				require.NotContains(t, string(actual[len(actual)-1]), manualOriginalText)
			} else {
				require.Zero(t, markers)
			}
			fixture.closeOwner(t)
		})
	}
}

// TestScenario checks owning cancellation through the live receive loop.
func (s *programmaticManualCancellationStopsNestedModelSuite) TestScenario() {
	const manualRequestWaitGuidance = "request-wait-for-caller-cancel"

	// Arrange both policy-owned and participant-owned nested work in the cancellation helper.
	// Act through public cancellation in each nested-work scenario.
	// Assert joined provider work in the cancellation helper.
	for _, guidance := range []string{manualFixtureWaitForCallerCancel, manualRequestWaitGuidance} {
		s.Run(guidance, func() {
			// Act through public cancellation in runProgrammaticManualNestedCancellation.
			// Assert joined provider work in runProgrammaticManualNestedCancellation.
			runProgrammaticManualNestedCancellation(s.T(), guidance, "")
		})
	}
}

// runProgrammaticManualNestedCancellation checks public termination of active nested provider work.
func runProgrammaticManualNestedCancellation(t *testing.T, guidance, participantTimeout string) {
	t.Helper()
	// Arrange nested provider work held until its owning lifetime ends.

	directory := buildManualPolicyFixture(t)
	settings := configuredRequestSettings
	if participantTimeout != "" {
		settings += "\ncompaction:\n  participantTimeout: " + participantTimeout + "\n"
	}
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	// started signals that nested provider work is active.
	started := make(chan struct{})
	// stopped signals that nested provider work has ended.
	stopped := make(chan struct{})
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			if bytes.Contains(encoded, []byte(manualFixtureNestedCancellationCheckpoint)) {
				close(started)
				<-request.Context().Done()
				close(stopped)
				return nil, request.Context().Err()
			}
			return &http.Response{
				Status:     "",
				StatusCode: http.StatusOK,
				Proto:      "",
				ProtoMajor: 0,
				ProtoMinor: 0,
				Header: make(
					http.Header,
				),
				Body:             io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
				ContentLength:    0,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          nil,
				TLS:              nil,
			}, nil
		})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))
	request := new(programmaticpb.OpenRequest)
	request.SetOperationId(manualFixtureCancelParent)
	programmaticRequest(
		request,
	).SetCompact(programmaticpb.Compact_builder{Instructions: new(guidance)}.Build())
	require.NoError(t, fixture.stream.Send(request))
	terminal, receiveErrors := receiveManualTerminal(fixture, manualFixtureCancelParent)

	// Act through caller cancellation or the configured participant deadline.
	select {
	case <-started:
		if participantTimeout == "" {
			require.NoError(
				t,
				fixture.stream.Send(cancelRequest(manualFixtureCancelCommand, manualFixtureCancelParent)),
			)
		}
	case event := <-terminal:
		require.Nil(
			t,
			event.GetCompleted().GetCompaction().GetCommitted(),
			"policy returned before nested work started",
		)
		require.FailNow(t, "policy did not start its nested model request")
	case err := <-receiveErrors:
		require.NoError(t, err)
	case <-t.Context().Done():
		require.FailNow(t, "application ended before nested work started")
	}
	var event *programmaticpb.HostEvent
	select {
	case event = <-terminal:
	case <-time.After(10 * time.Second):
		// Cancel the public operation so a missing Host deadline fails by assertion after cleanup.
		require.NoError(t, fixture.stream.Send(cancelRequest(manualFixtureCancelCommand, manualFixtureCancelParent)))
		event = <-terminal
	}

	// Assert joined nested work and preserved original history.
	require.True(t, event.HasCanceled() || event.HasFailed(), "parent must not complete a replacement")
	if event.HasFailed() {
		if participantTimeout == "" {
			require.Contains(t, event.GetFailed().GetMessage(), "context canceled")
		}
		require.Contains(t, event.GetFailed().GetMessage(), "request configured model")
	}
	if participantTimeout != "" {
		require.True(t, event.HasFailed(), "participant timeout must expose its failure")
		require.Contains(t, event.GetFailed().GetMessage(), "context deadline exceeded")
		require.Contains(t, event.GetFailed().GetMessage(), "provider catalog selection failed: not_found")
	}
	select {
	case <-stopped:
	default:
		assert.Fail(t, "parent ended before nested provider work stopped")
	}
	stored := sendProgrammaticOperation(
		t,
		fixture,
		manualFixtureCanceledOriginals,
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	require.Len(t, stored, 4)
	for _, entry := range stored {
		require.Nil(t, entry.GetCompaction())
	}
	fixture.closeOwner(t)
}

// TestProgrammaticManualParticipantTimeout checks Host expiry through the public operation.
func TestProgrammaticManualParticipantTimeout(t *testing.T) {
	const participantGuidance = "request-wait-for-caller-cancel"

	// Arrange real participant-owned nested model work and a YAML invocation duration.
	t.Setenv(manualFixtureGlyphReferencePolicyInput, filepath.Join(t.TempDir(), manualFixtureInputJson))
	// Act through Programmatic manual compaction without caller cancellation.
	// Assert joined provider cleanup, full timeout causes, and preserved history in the helper.
	runProgrammaticManualNestedCancellation(t, participantGuidance, "2s")
}
