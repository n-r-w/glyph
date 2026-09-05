//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestSummaryControlPreCommitFailureKeepsStoredTree verifies cancellation and invalid source change no durable state.
func TestSummaryControlPreCommitFailureKeepsStoredTree(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{summaryControlInvalidMode, summaryControlCancelMode} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			// Arrange a stored branch and a real extension that cancels or returns missing source metadata.
			paths := testPaths(t, summaryControlSettings)
			seedSummaryControlSession(t, paths)
			directory := t.TempDir()
			writeHandlerFixtureScript(t, directory, "control", mode, "")
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			t.Cleanup(fixture.cancel)
			resumeSummaryControlSession(t, fixture)
			before := readSummaryControlTree(t, fixture, "before")
			configure := func(request *programmaticv1.OpenRequest) {
				programmaticRequest(request).SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
					TargetEntryId: new(
						"user",
					),
					SummaryMode: new(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE),
					CustomFocus: nil,
				}.Build())
			}

			// Act through the public navigation operation and then restart Host.
			if mode == summaryControlInvalidMode {
				failure := sendProgrammaticFailed(t, fixture, "navigate", configure)
				assert.Contains(t, failure.GetMessage(), "branch summary requires exactly one source")
			} else {
				result := sendProgrammaticOperation(t, fixture, "navigate", configure).GetSessionTreeNavigation()
				require.Equal(
					t,
					programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED,
					result.GetStatus(),
				)
			}
			after := readSummaryControlTree(t, fixture, "after")
			fixture.closeOwner(t)
			restarted := startProgrammaticFixtureWithExtension(t, paths, directory)
			t.Cleanup(restarted.cancel)
			resumeSummaryControlSession(t, restarted)
			restored := readSummaryControlTree(t, restarted, "restored")
			restarted.closeOwner(t)

			// Assert both the active leaf and every persisted entry remain unchanged.
			assert.True(t, proto.Equal(before, after))
			assert.True(t, proto.Equal(before, restored))
		})
	}
}

// resumeSummaryControlSession binds the client to the seeded session.
func resumeSummaryControlSession(t *testing.T, fixture *programmaticFixture) {
	t.Helper()
	sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(
			request,
		).SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build())
	})
}

// readSummaryControlTree reads the full tree through Programmatic Control.
func readSummaryControlTree(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
) *programmaticv1.SessionTree {
	t.Helper()
	return sendProgrammaticOperation(t, fixture, operationID, func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetSessionTree(new(programmaticv1.GetSessionTree))
	}).GetSessionTree().GetTree()
}
