//go:build integration

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// summaryControlSettings keeps the active model usable while the unused summary model lacks credentials.
const summaryControlSettings = `defaultProvider: local
defaultModel: active
providers:
  local:
    type: openai-compatible
    baseURL: http://127.0.0.1:1/v1
    api: chat-completions
    models:
      - id: active
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing: {input: 1, output: 1, cacheRead: 1, cacheWrite: 1}
        reasoning: {supported: false, choices: [off], default: off}
      - id: priced
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing: {input: 3, output: 3, cacheRead: 3, cacheWrite: 3}
        reasoning: {supported: false, choices: [off], default: off}
  openai-codex:
    type: openai-codex
    models:
      - id: unused
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning: {supported: false, choices: [off], default: off}
`

// seedSummaryControlSession creates a real stored abandoned branch before clients connect.
func seedSummaryControlSession(t *testing.T, paths persistence.Paths) {
	t.Helper()
	canonical, err := sessionstore.CanonicalWorkingDirectory("")
	require.NoError(t, err)
	repository := sessionstore.New(filepath.Join(paths.Directory, "sessions"), canonical, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	_, err = repository.CreateSnapshot(t.Context(), hostsessions.CreateSnapshotCommand{
		Header: session.Header{Version: 2, ID: "source", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: canonical},
		Tree: grpcNavigationTree(
			t,
		),
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	})
	require.NoError(t, err)
}

// TestProgrammaticBranchSummaryControl verifies real extension errors, actual-source cost, and restart projection.
func TestProgrammaticBranchSummaryControl(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{summaryControlExtensionMode, summaryControlMissingMode, summaryControlModelMode} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			// Arrange stored branches and a real extension process with all three failure paths.
			paths := testPaths(t, summaryControlSettings)
			seedSummaryControlSession(t, paths)
			directory := t.TempDir()
			writeHandlerFixtureScript(t, directory, "control", mode, "")
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			t.Cleanup(fixture.cancel)
			sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
				programmaticRequest(
					request,
				).SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build())
			})
			var committed *programmaticv1.SessionTree
			for _, operationID := range []string{"navigate-first", "navigate-again"} {
				// Act twice to prove ordinary failures leave the real extension active.
				completed, progressEvents := sendProgrammaticOperationWithProgress(
					t, fixture, operationID, func(request *programmaticv1.OpenRequest) {
						programmaticRequest(request).SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
							TargetEntryId: new(
								"user",
							),
							SummaryMode: new(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE),
							CustomFocus: nil,
						}.Build())
					}, nil)
				result := completed.GetSessionTreeNavigation()

				// Assert progress precedes completion and post-error state still commits.
				require.NotNil(t, result)
				require.Equal(
					t,
					programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
					result.GetStatus(),
				)
				require.Len(t, result.GetIssues(), 3)
				for index, cause := range []string{summaryRequestCause, summaryResultCause, summaryObserverCause} {
					issue := result.GetIssues()[index]
					assert.Equal(t, cause, issue.GetMessage())
					assert.Equal(t, "control", issue.GetExtensionId())
					assert.Equal(
						t,
						[]string{"request-failure", "result-failure", "observer-failure"}[index],
						issue.GetHandlerId(),
					)
					code := programmaticv1.OperationIssueCode_OPERATION_ISSUE_CODE_HANDLER_ERROR
					if index == 2 {
						code = programmaticv1.OperationIssueCode_OPERATION_ISSUE_CODE_OBSERVER_ERROR
					}
					assert.Equal(t, code, issue.GetCode())
				}
				require.Len(t, progressEvents, 1)
				committed = progressEvents[0].GetSessionTreeNavigation().GetTree()
				entries := committed.GetEntries()
				last := entries[len(entries)-1]
				assert.Equal(t, last.GetId(), committed.GetActiveLeafId())
				assert.Equal(t, "root", result.GetDestinationId())
				assert.Equal(t, last.GetId(), result.GetActiveLeafId())
				assert.True(t, proto.Equal(last, result.GetCreatedSummary()))
				assertProgrammaticSummarySource(t, result.GetCreatedSummary().GetBranchSummary(), mode)
			}
			fixture.closeOwner(t)
			restarted := startProgrammaticFixtureWithExtension(t, paths, directory)
			t.Cleanup(restarted.cancel)
			sendProgrammaticOperation(t, restarted, "resume", func(request *programmaticv1.OpenRequest) {
				programmaticRequest(
					request,
				).SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build())
			})
			restored := sendProgrammaticOperation(t, restarted, "tree", func(request *programmaticv1.OpenRequest) {
				programmaticRequest(request).SetGetSessionTree(new(programmaticv1.GetSessionTree))
			}).GetSessionTree().GetTree()
			assert.True(t, proto.Equal(committed, restored))
			restarted.closeOwner(t)
		})
	}
}

// TestProgrammaticNavigationProgressReleasesBlockedObserver verifies progress crosses the real controller contract
// before an observer finishes.
func TestProgrammaticNavigationProgressReleasesBlockedObserver(t *testing.T) {
	t.Parallel()

	// Arrange a real Programmatic connection and extension observer blocked on a named pipe.
	paths := testPaths(t, summaryControlSettings)
	seedSummaryControlSession(t, paths)
	directory := t.TempDir()
	gate := filepath.Join(t.TempDir(), "observer-gate")
	require.NoError(t, syscall.Mkfifo(gate, 0o600))
	writeHandlerFixtureScript(t, directory, "control", summaryControlBlockedMode, gate)
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	t.Cleanup(fixture.cancel)
	sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetResumeSession(
			programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build(),
		)
	})
	var releaseErr error

	// Act by releasing the observer only from the typed navigation progress callback.
	completed, progressEvents := sendProgrammaticOperationWithProgress(
		t,
		fixture,
		"navigate-blocked",
		func(request *programmaticv1.OpenRequest) {
			programmaticRequest(request).SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
				TargetEntryId: new("user"),
				SummaryMode:   new(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE),
				CustomFocus:   nil,
			}.Build())
		},
		func(progress *programmaticv1.HostProgress) {
			if progress.HasSessionTreeNavigation() {
				releaseErr = os.WriteFile(gate, []byte("release"), 0o600)
			}
		},
	)

	// Assert progress was observable while the observer was blocked, then terminal metadata completed.
	require.NoError(t, releaseErr)
	require.Len(t, progressEvents, 1)
	progress := progressEvents[0].GetSessionTreeNavigation()
	require.NotNil(t, progress.GetTree())
	terminal := completed.GetSessionTreeNavigation()
	require.NotNil(t, terminal)
	require.Equal(t, progress.GetTree().GetActiveLeafId(), terminal.GetActiveLeafId())
	require.NotNil(t, terminal.GetCreatedSummary().GetBranchSummary())
	fixture.closeOwner(t)
}

// assertProgrammaticSummarySource checks attribution and accounting through the headless contract.
func assertProgrammaticSummarySource(t *testing.T, summary *programmaticv1.BranchSummary, mode string) {
	t.Helper()
	require.NotNil(t, summary)
	assert.Equal(t, "refined", summary.GetSummary())
	if mode == summaryControlModelMode {
		modelSource := summary.GetSource().GetModel()
		require.NotNil(t, modelSource)
		assert.Equal(t, "local", modelSource.GetProviderId())
		assert.Equal(t, "priced", modelSource.GetModelId())
		assert.Equal(t, int64(1000000), modelSource.GetUsage().GetInputTokens())
		require.True(t, summary.HasEstimatedCost())
		assert.InDelta(t, 3, summary.GetEstimatedCost().GetTotal(), 1e-12)
	} else {
		assert.Equal(t, summaryControlProducer, summary.GetSource().GetExtensionId())
		assert.False(t, summary.GetSource().HasModel())
		assert.False(t, summary.HasEstimatedCost())
	}
}
