//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// selectionFailureUIBehavior selects the public SDK selection failure caller.
	selectionFailureUIBehavior = "selection-failure"
	// selectionFailureKindEnvironment selects the public selection request kind.
	selectionFailureKindEnvironment = "GLYPH_SELECTION_FAILURE_KIND"
	// selectionFailureOperationID correlates the SDK wait and notification.
	selectionFailureOperationID = "selection-failure"
)

// selectionFailureUIReport retains the actual SDK wait and notification outcomes.
type selectionFailureUIReport struct {
	// Failure contains the complete typed wait failure.
	Failure string `json:"failure"`
	// Code contains the remote failure category.
	Code string `json:"code"`
	// Reasoning contains the retained committed selection, when present.
	Reasoning string `json:"reasoning"`
	// NotificationFailure contains the complete failed notification cause.
	NotificationFailure string `json:"notification_failure"`
	// NotificationReasoning contains the retained notification selection.
	NotificationReasoning string `json:"notification_reasoning"`
}

// runSelectionFailureUIFixture exercises the public SDK wait and notification against the real Host.
func runSelectionFailureUIFixture(ctx context.Context, host *uisdk.Host) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	request := new(uiv1.UIRequest)
	if os.Getenv(selectionFailureKindEnvironment) == "model" {
		request.SetSelectModel(uiv1.SelectModelCommand_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt-test"),
		}.Build())
	} else {
		request.SetSelectReasoningChoice(uiv1.SelectReasoningChoiceCommand_builder{
			Choice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build())
	}
	started, err := host.Start(ctx, selectionFailureOperationID, request)
	if err != nil {
		return err
	}
	report := selectionFailureUIReport{}
	result, failure := waitUIOperation(
		ctx,
		host,
		selectionFailureOperationID,
		started,
		func(notification *uisdk.Notification) {
			if notification.OperationID() == selectionFailureOperationID &&
				notification.Kind() == uisdk.NotificationFailed {
				report.NotificationFailure = notification.OperationError().Error()
				if retained := notification.Completed().GetModelSelection().GetSelection(); retained != nil {
					report.NotificationReasoning = retained.GetReasoningChoice().String()
				}
			}
		},
	)
	if failure == nil {
		return errors.New("selection unexpectedly completed without failure")
	}
	report.Failure = failure.Error()
	if remote, present := errors.AsType[*uisdk.FailureError](failure); present {
		report.Code = remote.Code()
	}
	if retained := result.GetModelSelection().GetSelection(); retained != nil {
		report.Reasoning = retained.GetReasoningChoice().String()
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err = os.WriteFile(os.Getenv(appUITraceEnvironment), encoded, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}

// TestPublicUISelectionFailurePreservesSDKOutcome verifies UI SDK consumers get Failed with the committed selection.
func TestPublicUISelectionFailurePreservesSDKOutcome(t *testing.T) {
	for _, kind := range []string{"model", "reasoning"} {
		for _, mode := range []string{
			"error", "invalid", "unavailable", "independent-cancel", "observer", "observer-invalid",
			"observer-unavailable", "observer-independent-cancel",
		} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				// Arrange a real UI SDK caller and extension selection participants.
				t.Setenv(appUIBehaviorEnvironment, selectionFailureUIBehavior)
				t.Setenv(externalSignalsEnvironment, t.TempDir())
				t.Setenv(selectionFailureKindEnvironment, kind)
				if strings.HasPrefix(mode, "observer") {
					t.Setenv(externalSelectionObserversEnvironment, "1")
					observerMode := strings.TrimPrefix(mode, "observer-")
					if observerMode == "observer" {
						observerMode = "error"
					}
					t.Setenv(externalSelectionObserverErrorEnvironment, observerMode)
					if kind == "model" {
						t.Setenv(externalSelectionCompositionEnvironment, "1")
					}
				} else {
					t.Setenv(externalSelectionCompositionEnvironment, "1")
					t.Setenv(externalSelectionFailureEnvironment, mode)
				}
				trace := filepath.Join(t.TempDir(), "selection.json")
				t.Setenv(appUITraceEnvironment, trace)
				settings := selectionCompositionSettings()
				if kind == "model" {
					settings = selectionFailureSettings()
				}
				paths := testPaths(t, settings)
				writeProgrammaticCredentials(t, paths)
				uiDirectory := t.TempDir()
				writeUIExecutable(t, uiDirectory, "Selection_UI")

				// Act through the actual UI and Extension streams in the assembled application.
				err := runWithPaths(t.Context(), paths, cli.Command{
					Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: buildPublicExtensionFixture(t),
					UIDirectory: uiDirectory, UIID: "selection-ui", SocketPath: "",
				}, &bytes.Buffer{}, &bytes.Buffer{})

				// Assert the wait and notification preserve failure and only Host-committed data.
				require.NoError(t, err)
				encoded, err := os.ReadFile(trace)
				require.NoError(t, err)
				var report selectionFailureUIReport
				require.NoError(t, json.Unmarshal(encoded, &report))
				expectedCode := "EXTENSION_FAILED"
				if mode == "unavailable" {
					expectedCode = "EXTENSION_UNAVAILABLE"
				}
				require.Equal(t, expectedCode, report.Code)
				require.NotEmpty(t, report.Failure)
				require.Equal(t, report.Failure, report.NotificationFailure)
				if strings.HasPrefix(mode, "observer") {
					if mode == "observer" {
						require.Contains(t, report.Failure, "public reasoning observer failed")
					}
					require.Equal(t, "REASONING_CHOICE_HIGH", report.Reasoning)
					require.Equal(t, report.Reasoning, report.NotificationReasoning)
				} else {
					require.Empty(t, report.Reasoning)
					require.Empty(t, report.NotificationReasoning)
				}
			})
		}
	}
}
