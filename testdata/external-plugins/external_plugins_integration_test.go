//go:build integration

// Package externalplugins_test verifies external plugins against only Glyph's public contracts.
package externalplugins_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// externalModulePath locates the separately versioned fixture module.
	externalModulePath = "."
	// externalTestTimeout bounds every subprocess and protocol operation in one test.
	externalTestTimeout = time.Minute
	// blockedObservationTime is long enough to observe an incorrectly early asynchronous result.
	blockedObservationTime = 100 * time.Millisecond
	// signalPollInterval bounds filesystem signal observation latency.
	signalPollInterval = 10 * time.Millisecond
	// clientCleanupTimeout bounds fallback go-plugin and child-process cleanup.
	clientCleanupTimeout = 10 * time.Second
	// invalidArgumentCode is the public rejection code used by both fixtures.
	invalidArgumentCode = "INVALID_ARGUMENT"
	// internalFailureCode is the public classified failure code used by both fixtures.
	internalFailureCode = "INTERNAL"
)

// callResult carries one bounded asynchronous public call result.
type callResult[T any] struct {
	// value is the public call result.
	value T
	// err is the complete public call error.
	err error
}

// TestExternalExtensionPlugin verifies an external module through the public Extension SDK and plugin protocol.
func TestExternalExtensionPlugin(t *testing.T) {
	t.Parallel()

	// Arrange a race-instrumented Extension command and one test-local operation deadline.
	ctx, cancel := context.WithTimeout(t.Context(), externalTestTimeout)
	defer cancel()
	binary := buildExternalCommand(t, ctx, "extension")
	requireRaceInstrumented(t, binary)
	signals := t.TempDir()
	command := exec.CommandContext(ctx, binary)
	childStderr := new(bytes.Buffer)
	command.Stderr = childStderr
	raceLogPath := filepath.Join(signals, "race-extension")
	command.Env = append(os.Environ(), "GLYPH_EXTERNAL_SIGNALS="+signals, "GORACE=log_path="+raceLogPath)
	client, err := extensionsdk.Connect(ctx, command)
	requireConnectSuccess(t, command, childStderr, raceLogPath, err)
	cleanup := registerClientCleanup(t, ctx, command, childStderr, raceLogPath, client.Close, client.Done(), signals,
		"cancel-cleanup-gate", "shutdown-cleanup-gate")
	connection, err := client.Open(ctx)
	require.NoError(t, err)

	// Act by completing registration and one ordinary Execute operation.
	registerRequest := new(extensionv1.HostRequest)
	registerRequest.SetRegister(new(extensionv1.RegisterRequest))
	registration, err := connection.Start(ctx, "register", registerRequest)
	require.NoError(t, err)
	registered, err := registration.Wait(ctx, nil)
	require.NoError(t, err)
	require.Len(t, registered.GetRegister().GetTools(), 1)
	ordinary, err := connection.Start(ctx, "ordinary", externalExecuteRequest("external", "ordinary"))
	require.NoError(t, err)
	ordinaryResult, err := ordinary.Wait(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "ordinary complete", ordinaryResult.GetTool().GetContents()[0].GetText())

	// Act by receiving public rejection and classified failure errors across the process boundary.
	rejected, err := connection.Start(ctx, "rejected", externalExecuteRequest("unknown", "ordinary"))
	require.NoError(t, err)
	_, rejectedErr := rejected.Wait(ctx, nil)
	requireExtensionRejection(t, rejectedErr)
	failed, err := connection.Start(ctx, "failed", externalExecuteRequest("external", "fail"))
	require.NoError(t, err)
	_, failedErr := failed.Wait(ctx, nil)
	requireExtensionFailure(t, failedErr)

	// Act by canceling blocked work while its process-visible Release gate remains closed.
	blocked, err := connection.Start(ctx, "blocked-cancel", externalExecuteRequest("external", "cancel"))
	require.NoError(t, err)
	waitSignal(t, ctx, signals, "cancel-run-started")
	blockedWait := startCall(func() (*extensionv1.ExtensionCompleted, error) {
		return blocked.Wait(ctx, nil)
	})
	cancellation, err := connection.Cancel(ctx, "cancel-blocked", "blocked-cancel")
	require.NoError(t, err)
	cancellationWait := startCall(func() (*operationv1.CancelCompleted, error) {
		return cancellation.Wait(ctx)
	})
	waitSignal(t, ctx, signals, "cancel-cleanup-started")
	requirePending(t, ctx, blockedWait, "blocked Extension Wait returned before Release completed")
	requirePending(t, ctx, cancellationWait, "Extension cancellation returned before target Release completed")
	writeSignal(t, signals, "cancel-cleanup-gate")
	waitSignal(t, ctx, signals, "cancel-cleanup-finished")
	blockedResult := awaitCall(t, ctx, blockedWait, "blocked Extension Wait")
	requireExtensionCanceled(t, blockedResult.err)
	cancellationResult := awaitCall(t, ctx, cancellationWait, "Extension cancellation")
	require.NoError(t, cancellationResult.err)
	require.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED,
		cancellationResult.value.GetTargetState())

	// Act by closing the connection while another Execute Release gate remains closed.
	shutdown, err := connection.Start(ctx, "blocked-shutdown", externalExecuteRequest("external", "shutdown"))
	require.NoError(t, err)
	waitSignal(t, ctx, signals, "shutdown-run-started")
	shutdownWait := startCall(func() (*extensionv1.ExtensionCompleted, error) {
		return shutdown.Wait(ctx, nil)
	})
	closeWait := startCall(func() (struct{}, error) {
		return struct{}{}, connection.Close()
	})
	waitSignal(t, ctx, signals, "shutdown-cleanup-started")
	requirePending(t, ctx, shutdownWait, "shutdown Extension Wait returned before Release completed")
	requirePending(t, ctx, closeWait, "Extension Close returned before Release completed")
	writeSignal(t, signals, "shutdown-cleanup-gate")
	waitSignal(t, ctx, signals, "shutdown-cleanup-finished")
	shutdownResult := awaitCall(t, ctx, shutdownWait, "shutdown Extension Wait")
	requireExtensionCanceled(t, shutdownResult.err)
	closeResult := awaitCall(t, ctx, closeWait, "Extension Close")
	require.NoError(t, closeResult.err)

	// Assert process cleanup and the child race result complete within a bounded cleanup context.
	require.NoError(t, cleanup.Close())
	require.True(t, client.Exited())
}

// TestExternalUIPlugin verifies an external module through the public UI SDK and plugin protocol.
func TestExternalUIPlugin(t *testing.T) {
	t.Parallel()

	// Arrange a race-instrumented UI command and one test-local operation deadline.
	ctx, cancel := context.WithTimeout(t.Context(), externalTestTimeout)
	defer cancel()
	binary := buildExternalCommand(t, ctx, "ui")
	requireRaceInstrumented(t, binary)
	signals := t.TempDir()
	command := exec.CommandContext(ctx, binary)
	childStderr := new(bytes.Buffer)
	command.Stderr = childStderr
	raceLogPath := filepath.Join(signals, "race-ui")
	command.Env = append(os.Environ(), "GLYPH_EXTERNAL_SIGNALS="+signals, "GORACE=log_path="+raceLogPath)
	client, err := uisdk.Connect(ctx, command)
	requireConnectSuccess(t, command, childStderr, raceLogPath, err)
	cleanup := registerClientCleanup(t, ctx, command, childStderr, raceLogPath, client.Close, client.Done(), signals,
		"run-cleanup-gate")
	stream, err := client.Service().Open(ctx)
	require.NoError(t, err)

	// Act by initializing the UI and completing its ordinary Host request.
	initialize := new(uiv1.HostRequest)
	initialize.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiHostRequest("initialize", initialize)))
	receiveUIOperationLifecycle(t, stream, "initialize")
	ordinary := receiveUIRequest(t, stream, "ordinary")
	require.NotNil(t, ordinary.GetRequest().GetSubmit())
	sendUIEvent(t, stream, "ordinary", acceptedHostEvent())
	sendUIEvent(t, stream, "ordinary", runningHostEvent())
	sendUIEvent(t, stream, "ordinary", completedSubmitHostEvent())

	// Act by returning one Rejected and one Failed Host operation to the external UI.
	rejected := receiveUIRequest(t, stream, "rejected")
	require.NotNil(t, rejected.GetRequest().GetSubmit())
	sendUIEvent(t, stream, "rejected", rejectedHostEvent())
	failed := receiveUIRequest(t, stream, "failed")
	require.NotNil(t, failed.GetRequest().GetSubmit())
	sendUIEvent(t, stream, "failed", acceptedHostEvent())
	sendUIEvent(t, stream, "failed", runningHostEvent())
	sendUIEvent(t, stream, "failed", failedHostEvent())

	// Act by requiring the next request as public-stream evidence that UI error checks passed.
	blocked := receiveUIRequest(t, stream, "blocked-cancel")
	require.NotNil(t, blocked.GetRequest().GetSubmit())
	cancelRequest := receiveUIRequest(t, stream, "cancel-blocked")
	require.Equal(t, "blocked-cancel", cancelRequest.GetRequest().GetCancel().GetTargetOperationId())
	sendUIEvent(t, stream, "blocked-cancel", acceptedHostEvent())
	sendUIEvent(t, stream, "blocked-cancel", runningHostEvent())
	sendUIEvent(t, stream, "cancel-blocked", acceptedHostEvent())
	sendUIEvent(t, stream, "cancel-blocked", runningHostEvent())
	waitSignal(t, ctx, signals, "target-wait-started")
	waitSignal(t, ctx, signals, "cancellation-wait-started")
	hostWorkStarted := make(chan struct{})
	hostWorkGate := make(chan struct{})
	hostWorkFinished := make(chan struct{})
	go func() {
		close(hostWorkStarted)
		select {
		case <-hostWorkGate:
		case <-ctx.Done():
		}
		close(hostWorkFinished)
	}()
	waitChannel(t, ctx, hostWorkStarted, "controlled Host work start")
	requireNoSignalFor(t, ctx, signals, "target-wait-finished",
		"target Wait completed while Host work remained active")
	requireNoSignalFor(t, ctx, signals, "cancellation-wait-finished",
		"cancellation Wait completed while Host work remained active")

	// Act by joining controlled Host work before publishing target and cancellation terminals.
	close(hostWorkGate)
	waitChannel(t, ctx, hostWorkFinished, "controlled Host work join")
	canceledEvent := new(uiv1.HostEvent)
	canceledEvent.SetCanceled(new(operationv1.Canceled))
	sendUIEvent(t, stream, "blocked-cancel", canceledEvent)
	waitSignal(t, ctx, signals, "target-wait-finished")
	requireNoSignalFor(t, ctx, signals, "cancellation-wait-finished",
		"cancellation Wait completed before its terminal event")
	sendUIEvent(t, stream, "cancel-blocked", completedCancelHostEvent())
	waitSignal(t, ctx, signals, "cancellation-wait-finished")
	waitSignal(t, ctx, signals, "cancel-joined")

	// Arrange controlled Host-owned work for the operation that remains active at requested shutdown.
	shutdown := receiveUIRequest(t, stream, "blocked-shutdown")
	require.NotNil(t, shutdown.GetRequest().GetSubmit())
	hostShutdownCtx, cancelHostShutdown := context.WithCancel(ctx)
	hostShutdownStarted := make(chan struct{})
	hostShutdownFinished := make(chan struct{})
	go func() {
		close(hostShutdownStarted)
		<-hostShutdownCtx.Done()
		close(hostShutdownFinished)
	}()
	waitChannel(t, ctx, hostShutdownStarted, "shutdown Host work start")
	sendUIEvent(t, stream, "blocked-shutdown", acceptedHostEvent())
	sendUIEvent(t, stream, "blocked-shutdown", runningHostEvent())
	waitSignal(t, ctx, signals, "shutdown-target-wait-started")
	streamEOF := startCall(func() (struct{}, error) {
		return struct{}{}, drainUIStream(stream)
	})

	// Act in Host-requested closure order: CloseConnection, work join, target terminal, then CloseSend.
	closeRequest := new(uiv1.OpenRequest)
	closeRequest.SetClose(new(operationv1.CloseConnection))
	require.NoError(t, stream.Send(closeRequest))
	waitSignal(t, ctx, signals, "run-cleanup-started")
	requireNoSignalFor(t, ctx, signals, "shutdown-target-wait-finished",
		"shutdown target Wait completed while Host work remained active")
	cancelHostShutdown()
	waitChannel(t, ctx, hostShutdownFinished, "shutdown Host work join")
	requireNoSignalFor(t, ctx, signals, "shutdown-target-wait-finished",
		"shutdown target Wait completed before its terminal event")
	shutdownCanceledEvent := new(uiv1.HostEvent)
	shutdownCanceledEvent.SetCanceled(new(operationv1.Canceled))
	sendUIEvent(t, stream, "blocked-shutdown", shutdownCanceledEvent)
	waitSignal(t, ctx, signals, "shutdown-target-wait-finished")
	closeSend := startCall(func() (struct{}, error) {
		return struct{}{}, stream.CloseSend()
	})
	closeSendResult := awaitCall(t, ctx, closeSend, "UI stream CloseSend")
	require.NoError(t, closeSendResult.err)

	// Assert UI-owned Run cleanup, Service.Close, EOF, and process exit remain ordered.
	requirePending(t, ctx, streamEOF, "UI stream reached EOF before Service.Run cleanup completed")
	requireNoSignalFor(t, ctx, signals, "service-close-started",
		"Service.Close started before Service.Run cleanup completed")
	writeSignal(t, signals, "run-cleanup-gate")
	waitSignal(t, ctx, signals, "run-cleanup-finished")
	waitSignal(t, ctx, signals, "service-close-started")
	waitSignal(t, ctx, signals, "service-close-finished")
	eofResult := awaitCall(t, ctx, streamEOF, "UI stream EOF")
	require.NoError(t, eofResult.err)
	require.NoError(t, cleanup.Close())
	require.True(t, client.Exited())
}
