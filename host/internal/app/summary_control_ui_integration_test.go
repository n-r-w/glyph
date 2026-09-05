//go:build integration

package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestUIBranchSummaryControl verifies real UI and extension processes expose sources and errors across restart.
func TestUIBranchSummaryControl(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{summaryControlExtensionMode, summaryControlMissingMode, summaryControlModelMode} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			// Arrange real storage, an extension process, and a UI process that navigates twice.
			paths := testPaths(t, summaryControlSettings)
			seedSummaryControlSession(t, paths)
			directory := t.TempDir()
			writeHandlerFixtureScript(t, directory, "control", mode, "")
			trace := filepath.Join(t.TempDir(), "navigation.json")

			// Act through UI Plugin Contract navigation and then restart with read-only tree retrieval.
			runSummaryControlUI(t, paths, directory, trace, "summary-control")
			data, err := os.ReadFile(trace)
			require.NoError(t, err)
			result := new(uiv1.SessionTreeNavigationResult)
			require.NoError(t, protojson.Unmarshal(data, result))

			// Assert all three complete causes and actual-source accounting reached the UI process.
			require.Equal(
				t,
				uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
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
				code := uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_HANDLER_ERROR
				if index == 2 {
					code = uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_OBSERVER_ERROR
				}
				assert.Equal(t, code, issue.GetCode())
			}
			entries := result.GetTree().GetEntries()
			require.Len(t, entries, 5)
			last := entries[len(entries)-1]
			assert.Equal(t, last.GetId(), result.GetTree().GetActiveLeafId())
			summary := last.GetBranchSummary()
			require.NotNil(t, summary)
			if mode == summaryControlModelMode {
				expected := uiv1.BranchSummaryModelSource_builder{
					ProviderId: new(
						"local",
					),
					ModelId:         new("priced"),
					ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_OFF),
					Usage: uiv1.TokenUsage_builder{
						InputTokens:     new(int64(1000000)),
						OutputTokens:    new(int64(0)),
						CacheReadTokens: new(int64(0)),
						CacheWriteTokens: new(
							int64(0),
						),
						ReasoningTokens: new(int64(0)),
						TotalTokens:     new(int64(1000000)),
					}.Build(),
				}.Build()
				assert.True(t, proto.Equal(expected, summary.GetSource().GetModel()))
				require.True(t, summary.HasEstimatedCost())
				assert.InDelta(t, 3, summary.GetEstimatedCost().GetTotal(), 1e-12)
			} else {
				assert.Equal(t, summaryControlProducer, summary.GetSource().GetExtensionId())
				assert.False(t, summary.GetSource().HasModel())
				assert.False(t, summary.HasEstimatedCost())
			}
			runSummaryControlUI(t, paths, directory, trace, "summary-read")
			data, err = os.ReadFile(trace)
			require.NoError(t, err)
			restored := new(uiv1.SessionTree)
			require.NoError(t, protojson.Unmarshal(data, restored))
			assert.True(t, proto.Equal(result.GetTree(), restored))
		})
	}
}

// runSummaryControlUI starts a fresh Host and one UI fixture process for a stored session.
func runSummaryControlUI(t *testing.T, paths persistence.Paths, extensions, trace, behavior string) {
	t.Helper()
	directory := t.TempDir()
	writeConfiguredUIExecutable(t, directory, "summary-ui", trace, behavior)
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: extensions,
		UIDirectory: directory, UIID: "summary-ui", SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, err)
}

// runSummaryControlUIFixture exercises UI commands and saves the received protobuf payload for parent assertions.
func runSummaryControlUIFixture(t *testing.T, ctx context.Context, host *uisdk.Host) error {
	t.Helper()
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	resume := new(uiv1.UIRequest)
	resume.SetResumeSession(uiv1.ResumeSessionCommand_builder{SessionId: new("source")}.Build())
	operation, err := host.Start(ctx, "resume", resume)
	if err != nil {
		return err
	}
	if _, err := operation.Wait(ctx, nil); err != nil {
		return err
	}
	var payload proto.Message
	if os.Getenv(appUIBehaviorEnvironment) == "summary-read" {
		request := new(uiv1.UIRequest)
		request.SetGetSessionTree(new(uiv1.GetSessionTreeCommand))
		operation, err := host.Start(ctx, "tree", request)
		if err != nil {
			return err
		}
		result, err := operation.Wait(ctx, nil)
		if err != nil {
			return err
		}
		return writeSummaryControlObservation(ctx, host, result.GetSessionTree().GetTree())
	}
	for _, id := range []string{"navigate-first", "navigate-again"} {
		request := new(uiv1.UIRequest)
		request.SetNavigateSessionTree(uiv1.NavigateSessionTreeCommand_builder{
			TargetEntryId: new("user"), SummaryMode: new(uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE), CustomFocus: nil,
		}.Build())
		operation, err := host.Start(ctx, id, request)
		if err != nil {
			return err
		}
		result, err := operation.Wait(ctx, nil)
		if err != nil {
			return err
		}
		payload = result.GetSessionTreeNavigation()
	}
	return writeSummaryControlObservation(ctx, host, payload)
}

// writeSummaryControlObservation saves the exact received wire payload before closing the UI connection.
func writeSummaryControlObservation(ctx context.Context, host *uisdk.Host, payload proto.Message) error {
	data, err := protojson.Marshal(payload)
	if err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv(appUITraceEnvironment), data, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}
