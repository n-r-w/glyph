//go:build integration

package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// ordinaryFailureUIBehavior selects the real SDK lifecycle failure scenario.
	ordinaryFailureUIBehavior = "ordinary-observer-failure"
	// ordinaryFailureUIOperation identifies the failing request.
	ordinaryFailureUIOperation = "ordinary-failure"
	// ordinaryHealthyUIOperation identifies the independent next request.
	ordinaryHealthyUIOperation = "ordinary-healthy"
)

// runOrdinaryFailureUIFixture checks actual failed SDK wait and notification followed by a healthy request.
func runOrdinaryFailureUIFixture(ctx context.Context, host *uisdk.Host) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("first request")}.Build())
	started, err := host.Start(ctx, ordinaryFailureUIOperation, request)
	if err != nil {
		return err
	}
	var notificationFailure error
	retainedModelText := ""
	idle := false
	_, failure := waitUIOperation(
		ctx,
		host,
		ordinaryFailureUIOperation,
		started,
		func(notification *uisdk.Notification) {
			if event := notification.ConnectionEvent(); event != nil &&
				event.GetAvailabilityChanged().GetAvailability() == uiv1.Availability_AVAILABILITY_IDLE {
				idle = true
			}
			if response := notification.Progress().GetAgentEvent().GetModelResponse(); response != nil {
				retainedModelText = response.GetText()
			}
			if notification.OperationID() == ordinaryFailureUIOperation &&
				notification.Kind() == uisdk.NotificationFailed {
				notificationFailure = notification.OperationError()
			}
		},
	)
	if failure == nil {
		return errors.New("ordinary observer unexpectedly completed")
	}
	remote, ok := errors.AsType[*uisdk.FailureError](failure)
	if !ok || remote.Code() != "EXTENSION_FAILED" {
		return errors.Join(errors.New("ordinary observer failure category missing"), failure)
	}
	if notificationFailure == nil || notificationFailure.Error() != failure.Error() {
		return errors.Join(errors.New("failed notification differs from SDK wait"), failure, notificationFailure)
	}
	signals := os.Getenv(externalSignalsEnvironment)
	if _, err = os.Stat(filepath.Join(signals, ordinaryObserverLaterSignal)); !errors.Is(err, os.ErrNotExist) {
		return errors.New("later observer ran in failed chain")
	}
	if !idle {
		if err = waitForIdle(ctx, host); err != nil {
			return err
		}
	}
	request = new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("second request")}.Build())
	started, err = host.Start(ctx, ordinaryHealthyUIOperation, request)
	if err != nil {
		return err
	}
	if _, err = waitUIOperation(ctx, host, ordinaryHealthyUIOperation, started, nil); err != nil {
		return err
	}
	if err = os.WriteFile(
		os.Getenv(appUITraceEnvironment),
		[]byte(failure.Error()+"\n"+retainedModelText),
		0o600,
	); err != nil {
		return err
	}
	return host.Close(ctx)
}

// TestPublicUIOrdinaryObserverFailure verifies terminal failure and reuse through a real UI SDK process.
func TestPublicUIOrdinaryObserverFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		kind extensionv1.HandlerKind
	}{
		{name: "AGENT_START", kind: extensionv1.HandlerKind_HANDLER_KIND_AGENT_START},
		{name: "AGENT_END", kind: extensionv1.HandlerKind_HANDLER_KIND_AGENT_END},
		{name: "AGENT_SETTLED", kind: extensionv1.HandlerKind_HANDLER_KIND_AGENT_SETTLED},
	} {
		kind := test.name
		t.Run(kind, func(t *testing.T) {
			// Arrange real UI and extension processes with one failing lifecycle chain.
			t.Setenv(appUIBehaviorEnvironment, ordinaryFailureUIBehavior)
			t.Setenv(ordinaryObserverEnvironment, test.kind.String())
			t.Setenv(externalSignalsEnvironment, t.TempDir())
			trace := filepath.Join(t.TempDir(), "failure.txt")
			t.Setenv(appUITraceEnvironment, trace)
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			bodies := ordinaryObserverTransport(t)
			uiDirectory := t.TempDir()
			writeUIExecutable(t, uiDirectory, "Ordinary_UI")

			// Act through the assembled Host and both public plugin contracts.
			err := runWithPaths(
				t.Context(),
				paths,
				cli.Command{
					Mode:               cli.ModeUI,
					Headless:           headless.Command{},
					ExtensionDirectory: buildPublicExtensionFixture(t),
					UIDirectory:        uiDirectory,
					UIID:               "ordinary-ui",
					SocketPath:         "",
				},
				&bytes.Buffer{},
				&bytes.Buffer{},
			)

			// Assert the SDK receives the full failure and the next operation completes.
			require.NoError(t, err)
			report, err := os.ReadFile(trace)
			require.NoError(t, err)
			require.Contains(t, string(report), ordinaryObserverCause)
			if kind != "AGENT_START" {
				require.Contains(t, string(report), "Request complete.")
			}
			expected := 2
			if kind == "AGENT_START" {
				expected = 1
			}
			require.Len(t, bodies.snapshot(), expected)
		})
	}
}
