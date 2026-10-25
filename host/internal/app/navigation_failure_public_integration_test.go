//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// Navigation mode components classify committed and failure expectations.
	navigationObserverPrefix = "observer"
	navigationErrorSuffix    = "error"
	navigationConsumedSuffix = "consumed"
	// navigationSelectionCompositionEnabled enables consumed-child fixture composition.
	navigationSelectionCompositionEnabled = "1"
	// navigationSelectionFailureMode selects the child selection handler failure.
	navigationSelectionFailureMode = "error"
	// navigationOrdinaryToolMode selects ordinary external tool behavior.
	navigationOrdinaryToolMode = "ordinary"
	// Navigation failure modes select the participant and failure in both public suites.
	navigationRequestError        = "request-error"
	navigationObserverError       = "observer-error"
	navigationRequestInvalid      = "request-invalid"
	navigationObserverInvalid     = "observer-invalid"
	navigationRequestConsumed     = "request-consumed"
	navigationObserverConsumed    = "observer-consumed"
	navigationResultError         = "result-error"
	navigationResultInvalid       = "result-invalid"
	navigationRequestUnavailable  = "request-unavailable"
	navigationObserverUnavailable = "observer-unavailable"
	navigationRequestCancel       = "request-cancel"
	navigationObserverCancel      = "observer-cancel"
	// Navigation signal expectations identify stopped and healthy participants.
	navigationLaterSignal   = "navigation-later"
	navigationHealthySignal = "navigation-healthy"
	// navigationFailureCategory is the public extension failure expectation.
	navigationFailureCategory = "EXTENSION_FAILED"
	// navigationFailureEnvironment selects the external navigation failure.
	navigationFailureEnvironment = "GLYPH_NAVIGATION_FAILURE"
	// navigationFailureCause is the external participant's complete original diagnostic.
	navigationFailureCause = "navigation participant full unique failure: original source Ω"
)

// TestPublicNavigationFailureStopsAndRetainsCommit verifies real navigation terminals and persisted state.
func TestPublicNavigationFailureStopsAndRetainsCommit(t *testing.T) {
	for _, mode := range []string{
		navigationRequestError, navigationObserverError, navigationRequestInvalid, navigationObserverInvalid,
		navigationRequestConsumed, navigationObserverConsumed, navigationResultError, navigationResultInvalid,
		navigationRequestUnavailable, navigationObserverUnavailable, navigationRequestCancel, navigationObserverCancel,
	} {
		t.Run(mode, func(t *testing.T) {
			// Arrange an assembled Programmatic application with a real external extension process.
			t.Setenv(navigationFailureEnvironment, mode)
			if mode == navigationRequestConsumed || mode == navigationObserverConsumed {
				t.Setenv(externalSelectionCompositionEnvironment, navigationSelectionCompositionEnabled)
				t.Setenv(externalSelectionFailureEnvironment, navigationSelectionFailureMode)
			}
			signals := t.TempDir()
			t.Setenv(externalSignalsEnvironment, signals)
			settings := codexSettings("")
			if mode == navigationRequestConsumed || mode == navigationObserverConsumed {
				settings = selectionCompositionSettings()
			}
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			var calls atomic.Int32
			var body atomic.Value
			previous := http.DefaultTransport
			http.DefaultTransport = catalogueProviderTransport(
				t,
				&calls,
				&body,
				func() string { return navigationOrdinaryToolMode },
			)
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
			defer fixture.closeOwner(t)
			completeProgrammaticRequest(t, fixture, userRequest("history", "root input"))
			before := sendProgrammaticOperation(t, fixture, "before", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionTree(new(programmaticpb.GetSessionTree))
			}).GetSessionTree().GetTree()
			target := before.GetEntries()[0].GetId()
			request := new(programmaticpb.OpenRequest)
			request.SetOperationId("navigation-failure")
			programmaticRequest(request).SetNavigateSessionTree(programmaticpb.NavigateSessionTree_builder{
				TargetEntryId: new(
					target,
				),
				SummaryMode: new(programmaticpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
				CustomFocus: nil,
			}.Build())

			// Act through the actual public stream and observe its one terminal.
			require.NoError(t, fixture.stream.Send(request))
			var terminal *programmaticpb.HostEvent
			for terminal == nil {
				response, err := fixture.stream.Recv()
				require.NoError(t, err)
				if response.GetOperationId() != request.GetOperationId() {
					continue
				}
				event := response.GetEvent()
				if event.HasFailed() || event.HasCompleted() || event.HasCanceled() {
					terminal = event
				}
			}

			// Assert complete failure, stopped later participants, and actual root commit versus unchanged state.
			failed := terminal.GetFailed()
			require.NotNil(t, failed)
			require.Equal(t, navigationFailureCategory, failed.GetFailure().GetCode())
			if mode == navigationRequestError || mode == navigationObserverError {
				require.Contains(t, failed.GetFailure().GetMessage(), navigationFailureCause)
			}
			if mode == navigationRequestConsumed || mode == navigationObserverConsumed {
				require.Contains(t, failed.GetFailure().GetMessage(), publicSelectionHandlerFailureText)
			}
			_, err := os.Stat(filepath.Join(signals, navigationLaterSignal))
			require.ErrorIs(t, err, os.ErrNotExist)
			after := sendProgrammaticOperation(t, fixture, "after", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionTree(new(programmaticpb.GetSessionTree))
			}).GetSessionTree().GetTree()
			committed := strings.HasPrefix(mode, navigationObserverPrefix)
			if committed {
				result := failed.GetResult().GetSessionTreeNavigation()
				require.NotNil(t, result)
				require.Equal(
					t,
					programmaticpb.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
					result.GetStatus(),
				)
				require.Empty(t, result.GetDestinationId())
				require.Empty(t, result.GetActiveLeafId())
				require.Empty(t, after.GetActiveLeafId())
				require.Equal(t, "root input", result.GetNextInput())
			} else {
				require.Nil(t, failed.GetResult())
				require.True(t, proto.Equal(before, after))
			}
			// Assert Host completes independent navigation after ordinary failure or selected process loss.
			healthy := new(programmaticpb.OpenRequest)
			healthy.SetOperationId("healthy-navigation")
			programmaticRequest(
				healthy,
			).SetNavigateSessionTree(programmaticpb.NavigateSessionTree_builder{
				TargetEntryId: new(
					target,
				),
				SummaryMode: new(programmaticpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
				CustomFocus: nil,
			}.Build())
			require.NotNil(t, completeProgrammaticRequest(t, fixture, healthy).GetSessionTreeNavigation())
			if strings.HasSuffix(mode, navigationErrorSuffix) {
				_, healthyErr := os.Stat(filepath.Join(signals, navigationHealthySignal))
				require.NoError(t, healthyErr)
			}
		})
	}
}
