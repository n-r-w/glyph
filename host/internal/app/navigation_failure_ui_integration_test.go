//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// navigationFailureUIBehavior selects the real UI SDK navigation caller.
const navigationFailureUIBehavior = "navigation-failure"

// navigationFailureUIReport records SDK outcomes and publicly queried persisted state.
type navigationFailureUIReport struct {
	// Failure contains the complete SDK wait error.
	Failure string `json:"failure"`
	// Code contains the SDK's current failure category.
	Code string `json:"code"`
	// Healthy confirms participation by the same extension after an ordinary failure.
	Healthy bool `json:"healthy"`
	// NotificationFailure contains the complete SDK notification error.
	NotificationFailure string `json:"notification_failure"`
	// Committed records presence of the typed navigation result.
	Committed bool `json:"committed"`
	// NotificationCommitted records presence of the notification result.
	NotificationCommitted bool `json:"notification_committed"`
	// Unchanged compares public tree snapshots after precommit failure.
	Unchanged bool `json:"unchanged"`
	// ActiveLeaf contains the persisted active leaf after navigation.
	ActiveLeaf string `json:"active_leaf"`
	// NextInput contains exact committed editable input.
	NextInput string `json:"next_input"`
	// Later records whether Host invoked the later selected participant before independent recovery.
	Later bool `json:"later"`
}

// runNavigationFailureUIFixture exercises navigation through a real UI SDK process.
func runNavigationFailureUIFixture(ctx context.Context, host *uisdk.Host) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	submit := new(uiv1.UIRequest)
	submit.SetSubmit(uiv1.SubmitCommand_builder{Text: new("root input")}.Build())
	started, err := host.Start(ctx, "history", submit)
	if err != nil {
		return err
	}
	idle := false
	_, err = waitUIOperation(ctx, host, "history", started, func(notification *uisdk.Notification) {
		if notification.ConnectionEvent().
			GetAvailabilityChanged().
			GetAvailability() ==
			uiv1.Availability_AVAILABILITY_IDLE {
			idle = true
		}
	})
	if err != nil {
		return err
	}
	if !idle {
		if err = waitForIdle(ctx, host); err != nil {
			return err
		}
	}
	read := func(id string) (*uiv1.SessionTree, error) {
		request := new(uiv1.UIRequest)
		request.SetGetSessionTree(new(uiv1.GetSessionTreeCommand))
		operation, startErr := host.Start(ctx, id, request)
		if startErr != nil {
			return nil, startErr
		}
		result, waitErr := waitUIOperation(ctx, host, id, operation, nil)
		return result.GetSessionTree().GetTree(), waitErr
	}
	before, err := read("before")
	if err != nil {
		return err
	}
	if len(before.GetEntries()) == 0 {
		return errors.New("navigation fixture history is empty")
	}
	target := before.GetEntries()[0].GetId()
	request := new(uiv1.UIRequest)
	request.SetNavigateSessionTree(
		uiv1.NavigateSessionTreeCommand_builder{
			TargetEntryId: new(target),
			SummaryMode:   new(uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
			CustomFocus:   nil,
		}.Build(),
	)
	started, err = host.Start(ctx, "navigation-failure", request)
	if err != nil {
		return err
	}
	report := navigationFailureUIReport{
		Failure: "",
		Code:    "", Healthy: false,
		NotificationFailure:   "",
		Committed:             false,
		NotificationCommitted: false,
		Unchanged:             false,
		ActiveLeaf:            "",
		NextInput:             "",
		Later:                 false,
	}
	result, failure := waitUIOperation(
		ctx,
		host,
		"navigation-failure",
		started,
		func(notification *uisdk.Notification) {
			if notification.Kind() == uisdk.NotificationFailed {
				report.NotificationFailure = notification.OperationError().Error()
				report.NotificationCommitted = notification.Completed().GetSessionTreeNavigation() != nil
			}
		},
	)
	if failure == nil {
		return errors.New("navigation unexpectedly completed without failure")
	}
	report.Failure = failure.Error()
	if remote, found := errors.AsType[*uisdk.FailureError](failure); found {
		report.Code = remote.Code()
	}
	navigation := result.GetSessionTreeNavigation()
	report.Committed = navigation != nil
	report.NextInput = navigation.GetNextInput()
	after, err := read("after")
	if err != nil {
		return err
	}
	report.Unchanged = proto.Equal(before, after)
	report.ActiveLeaf = after.GetActiveLeafId()
	_, signalErr := os.Stat(filepath.Join(os.Getenv(externalSignalsEnvironment), navigationLaterSignal))
	report.Later = signalErr == nil
	if signalErr != nil && !errors.Is(signalErr, os.ErrNotExist) {
		return signalErr
	}
	// Host must complete independent navigation after ordinary failure or selected process loss.
	started, err = host.Start(ctx, "healthy-navigation", request)
	if err != nil {
		return err
	}
	if _, err = waitUIOperation(ctx, host, "healthy-navigation", started, nil); err != nil {
		return err
	}
	_, healthyErr := os.Stat(filepath.Join(os.Getenv(externalSignalsEnvironment), navigationHealthySignal))
	report.Healthy = healthyErr == nil
	if healthyErr != nil && !errors.Is(healthyErr, os.ErrNotExist) {
		return healthyErr
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

// TestPublicUINavigationFailurePreservesSDKOutcome verifies Failed result-plus-error and persisted state.
func TestPublicUINavigationFailurePreservesSDKOutcome(t *testing.T) {
	for _, mode := range []string{
		navigationRequestError, navigationObserverError, navigationRequestInvalid, navigationObserverInvalid,
		navigationRequestConsumed, navigationObserverConsumed, navigationResultError, navigationResultInvalid,
		navigationRequestUnavailable, navigationObserverUnavailable, navigationRequestCancel, navigationObserverCancel,
	} {
		t.Run(mode, func(t *testing.T) {
			// Arrange real UI and extension processes with a selected failing participant.
			t.Setenv(appUIBehaviorEnvironment, navigationFailureUIBehavior)
			t.Setenv(navigationFailureEnvironment, mode)
			t.Setenv(externalSignalsEnvironment, t.TempDir())
			settings := codexSettings("")
			if strings.HasSuffix(mode, navigationConsumedSuffix) {
				settings = selectionCompositionSettings()
				t.Setenv(externalSelectionCompositionEnvironment, navigationSelectionCompositionEnabled)
				t.Setenv(externalSelectionFailureEnvironment, navigationSelectionFailureMode)
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
			trace := filepath.Join(t.TempDir(), "navigation.json")
			t.Setenv(appUITraceEnvironment, trace)
			directory := t.TempDir()
			writeUIExecutable(t, directory, "Navigation_UI")

			// Act through the assembled application and the public UI SDK.
			err := runWithPaths(
				t.Context(),
				paths,
				cli.Command{
					Mode:               cli.ModeUI,
					Headless:           headless.Command{},
					ExtensionDirectory: buildPublicExtensionFixture(t),
					UIDirectory:        directory,
					UIID:               "navigation-ui",
					SocketPath:         "",
				},
				&bytes.Buffer{},
				&bytes.Buffer{},
			)

			// Assert wait and notification retain identical full errors and only actual committed results.
			require.NoError(t, err)
			encoded, err := os.ReadFile(trace)
			require.NoError(t, err)
			var report navigationFailureUIReport
			require.NoError(t, json.Unmarshal(encoded, &report))
			require.NotEmpty(t, report.Failure)
			require.Equal(t, navigationFailureCategory, report.Code)
			if strings.HasSuffix(mode, navigationErrorSuffix) {
				require.True(t, report.Healthy)
			}
			require.Equal(t, report.Failure, report.NotificationFailure)
			require.False(t, report.Later)
			if strings.HasSuffix(mode, navigationErrorSuffix) {
				require.Contains(t, report.Failure, navigationFailureCause)
			}
			if strings.HasSuffix(mode, navigationConsumedSuffix) {
				require.Contains(t, report.Failure, publicSelectionHandlerFailureText)
			}
			committed := strings.HasPrefix(mode, navigationObserverPrefix)
			require.Equal(t, committed, report.Committed)
			require.Equal(t, committed, report.NotificationCommitted)
			if committed {
				require.Empty(t, report.ActiveLeaf)
				require.Equal(t, "root input", report.NextInput)
			} else {
				require.True(t, report.Unchanged)
			}
		})
	}
}
