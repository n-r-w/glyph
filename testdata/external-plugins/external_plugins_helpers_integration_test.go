//go:build integration

// Package externalplugins_test verifies external plugins against only Glyph's public contracts.
package externalplugins_test

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// clientCleanup owns idempotent bounded cleanup for one go-plugin child.
type clientCleanup struct {
	// baseContext supplies values for cleanup without inheriting test cancellation.
	baseContext context.Context
	// command exposes the final child process state.
	command *exec.Cmd
	// stderr preserves complete caller-owned child diagnostics.
	stderr *bytes.Buffer
	// raceLogPath prefixes process-visible child race reports.
	raceLogPath string
	// close joins go-plugin transport and process resources.
	close func()
	// done closes after the child process terminates.
	done <-chan struct{}
	// signals contains process cleanup gates.
	signals string
	// gates lists every gate that fallback cleanup must open.
	gates []string
	// once makes explicit and registered cleanup idempotent.
	once sync.Once
	// err preserves all gate, client, process, and race-report cleanup errors.
	err error
}

// requireConnectSuccess reports one complete startup failure before client cleanup can exist.
func requireConnectSuccess(
	t *testing.T,
	command *exec.Cmd,
	stderr *bytes.Buffer,
	raceLogPath string,
	connectErr error,
) {
	t.Helper()
	if connectErr == nil {
		return
	}
	errorsFound := []error{fmt.Errorf("public SDK Connect failed: %w", connectErr)}
	if command.ProcessState == nil {
		errorsFound = append(errorsFound, errors.New("child process state is unavailable"))
	} else {
		errorsFound = append(errorsFound, fmt.Errorf("child process state: %s", command.ProcessState.String()))
	}
	if stderr.Len() != 0 {
		errorsFound = append(errorsFound, errors.New("child stderr:\n"+stderr.String()))
	}
	reports, reportErr := readRaceReports(raceLogPath)
	if reportErr != nil {
		errorsFound = append(errorsFound, reportErr)
	}
	if reports != "" {
		errorsFound = append(errorsFound, errors.New("startup race reports:\n"+reports))
	}
	require.NoError(t, errors.Join(errorsFound...))
}

// registerClientCleanup registers bounded cleanup immediately after a successful Connect.
func registerClientCleanup(
	t *testing.T,
	ctx context.Context,
	command *exec.Cmd,
	stderr *bytes.Buffer,
	raceLogPath string,
	closeFn func(),
	done <-chan struct{},
	signals string,
	gates ...string,
) *clientCleanup {
	t.Helper()
	cleanup := &clientCleanup{
		baseContext: ctx,
		command:     command,
		stderr:      stderr,
		raceLogPath: raceLogPath,
		close:       closeFn,
		done:        done,
		signals:     signals,
		gates:       gates,
		once:        sync.Once{},
		err:         nil,
	}
	t.Cleanup(func() {
		if err := cleanup.Close(); err != nil {
			t.Errorf("clean up external plugin client: %v", err)
		}
	})
	return cleanup
}

// Close opens cleanup gates, joins the client and process, and checks the child race result.
func (cleanup *clientCleanup) Close() error {
	cleanup.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(cleanup.baseContext), clientCleanupTimeout)
		defer cancel()
		errorsFound := make([]error, 0, len(cleanup.gates)+3)
		for _, gate := range cleanup.gates {
			if err := os.WriteFile(filepath.Join(cleanup.signals, gate), nil, 0o600); err != nil {
				errorsFound = append(errorsFound, fmt.Errorf("open cleanup gate %q: %w", gate, err))
			}
		}

		closeCall := startCall(func() (struct{}, error) {
			cleanup.close()
			return struct{}{}, nil
		})
		select {
		case <-closeCall.started:
		case <-ctx.Done():
			errorsFound = append(errorsFound, fmt.Errorf("start client cleanup: %w", context.Cause(ctx)))
		}
		select {
		case result := <-closeCall.result:
			if result.err != nil {
				errorsFound = append(errorsFound, fmt.Errorf("close plugin client: %w", result.err))
			}
		case <-ctx.Done():
			errorsFound = append(errorsFound, fmt.Errorf("close plugin client: %w", context.Cause(ctx)))
		}
		select {
		case <-cleanup.done:
			errorsFound = append(errorsFound,
				processExitError(cleanup.command, cleanup.stderr.String(), cleanup.raceLogPath))
		case <-ctx.Done():
			errorsFound = append(errorsFound, fmt.Errorf("wait for child process exit: %w", context.Cause(ctx)))
		}
		cleanup.err = errors.Join(errorsFound...)
	})
	return cleanup.err
}

// processExitError requires a successful exit and includes complete child stderr and race reports on failure.
func processExitError(command *exec.Cmd, stderr, raceLogPath string) error {
	if command.ProcessState == nil {
		return errors.New("child process state is required after process completion")
	}
	if command.ProcessState.Success() {
		return nil
	}
	exitText := fmt.Sprintf("child process exited unsuccessfully: %s", command.ProcessState.String())
	if stderr != "" {
		exitText += "\nchild stderr:\n" + stderr
	}
	exitErr := errors.New(exitText)
	reports, reportErr := readRaceReports(raceLogPath)
	if reportErr != nil {
		return errors.Join(exitErr, reportErr)
	}
	if reports == "" {
		return exitErr
	}
	return fmt.Errorf("%w\nchild race reports:\n%s", exitErr, reports)
}

// readRaceReports reads every race-runtime file for one child without truncation.
func readRaceReports(raceLogPath string) (string, error) {
	paths, err := filepath.Glob(raceLogPath + ".*")
	if err != nil {
		return "", fmt.Errorf("find child race reports: %w", err)
	}
	if _, statErr := os.Stat(raceLogPath); statErr == nil {
		paths = append(paths, raceLogPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect child race report %q: %w", raceLogPath, statErr)
	}
	sort.Strings(paths)
	var reports strings.Builder
	for _, path := range paths {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return reports.String(), fmt.Errorf("read child race report %q: %w", path, readErr)
		}
		_, _ = fmt.Fprintf(&reports, "%s:\n%s", path, content)
		if len(content) == 0 || content[len(content)-1] != '\n' {
			reports.WriteByte('\n')
		}
	}
	return reports.String(), nil
}

// buildExternalCommand builds one race-instrumented command from the separate fixture module.
func buildExternalCommand(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name)
	command := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/"+name)
	command.Dir = externalModulePath
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return binary
}

// requireRaceInstrumented checks the command's Go build metadata for race instrumentation.
func requireRaceInstrumented(t *testing.T, binary string) {
	t.Helper()
	information, err := buildinfo.ReadFile(binary)
	require.NoError(t, err)
	for _, setting := range information.Settings {
		if setting.Key == "-race" {
			require.Equal(t, "true", setting.Value)
			return
		}
	}
	t.Fatal("external command metadata does not contain race instrumentation")
}

// externalExecuteRequest creates one mode-specific public Execute request.
func externalExecuteRequest(tool, mode string) *extensionv1.HostRequest {
	request := new(extensionv1.HostRequest)
	request.SetExecute(extensionv1.ExecuteRequest_builder{
		ToolName: new(tool), ArgumentsJson: []byte(`{"mode":"` + mode + `"}`),
	}.Build())
	return request
}

// requireExtensionRejection checks the complete public Extension rejection contract.
func requireExtensionRejection(t *testing.T, err error) {
	t.Helper()
	var rejection *extensionsdk.RejectionError
	require.ErrorAs(t, err, &rejection)
	require.Equal(t, invalidArgumentCode, rejection.Code())
	require.EqualError(t, err, "external fixture tool name is invalid")
	require.EqualError(t, errors.Unwrap(err), "external fixture tool name is invalid")
}

// requireExtensionFailure checks the complete public Extension failure contract.
func requireExtensionFailure(t *testing.T, err error) {
	t.Helper()
	var failure *extensionsdk.FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, internalFailureCode, failure.Code())
	require.EqualError(t, err, "complete external Extension failure")
	require.EqualError(t, errors.Unwrap(err), "complete external Extension failure")
}

// requireExtensionCanceled checks the public Extension cancellation type.
func requireExtensionCanceled(t *testing.T, err error) {
	t.Helper()
	var canceled *extensionsdk.CanceledError
	require.ErrorAs(t, err, &canceled)
	require.ErrorIs(t, err, context.Canceled)
}

// asyncCall exposes separate started and completion barriers for one public call.
type asyncCall[T any] struct {
	// started closes immediately before the goroutine enters the public call.
	started <-chan struct{}
	// result receives the public call result.
	result <-chan callResult[T]
}

// startCall starts one public call whose entry and completion must be observed separately.
func startCall[T any](call func() (T, error)) asyncCall[T] {
	started := make(chan struct{})
	result := make(chan callResult[T], 1)
	go func() {
		close(started)
		value, err := call()
		result <- callResult[T]{value: value, err: err}
	}()
	return asyncCall[T]{started: started, result: result}
}

// awaitCall waits for call entry and completion within the test-local deadline.
func awaitCall[T any](t *testing.T, ctx context.Context, call asyncCall[T], name string) callResult[T] {
	t.Helper()
	waitChannel(t, ctx, call.started, name+" start")
	select {
	case completed := <-call.result:
		return completed
	case <-ctx.Done():
		t.Fatalf("%s did not complete: %v", name, context.Cause(ctx))
		return callResult[T]{}
	}
}

// requirePending proves a started public call remains blocked during a bounded observation window.
func requirePending[T any](t *testing.T, ctx context.Context, call asyncCall[T], message string) {
	t.Helper()
	waitChannel(t, ctx, call.started, "pending public call start")
	timer := time.NewTimer(blockedObservationTime)
	defer timer.Stop()
	select {
	case completed := <-call.result:
		t.Fatalf("%s: %v", message, completed.err)
	case <-timer.C:
	case <-ctx.Done():
		t.Fatalf("observe blocked call: %v", context.Cause(ctx))
	}
}

// waitChannel waits for a process or controlled-work signal within the test-local deadline.
func waitChannel(t *testing.T, ctx context.Context, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v", name, context.Cause(ctx))
	}
}

// waitSignal waits for one child-process file signal within the test-local deadline.
func waitSignal(t *testing.T, ctx context.Context, directory, name string) {
	t.Helper()
	path := filepath.Join(directory, name)
	ticker := time.NewTicker(signalPollInterval)
	defer ticker.Stop()
	for {
		_, err := os.Stat(path)
		if err == nil {
			return
		}
		require.ErrorIs(t, err, os.ErrNotExist)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("wait for signal %q: %v", name, context.Cause(ctx))
		}
	}
}

// requireNoSignalFor proves one child signal remains absent during a bounded observation window.
func requireNoSignalFor(t *testing.T, ctx context.Context, directory, name, message string) {
	t.Helper()
	path := filepath.Join(directory, name)
	timer := time.NewTimer(blockedObservationTime)
	defer timer.Stop()
	ticker := time.NewTicker(signalPollInterval)
	defer ticker.Stop()
	for {
		_, err := os.Stat(path)
		if err == nil {
			t.Fatal(message)
		}
		require.ErrorIs(t, err, os.ErrNotExist)
		select {
		case <-ticker.C:
		case <-timer.C:
			return
		case <-ctx.Done():
			t.Fatalf("observe absent signal %q: %v", name, context.Cause(ctx))
		}
	}
}

// writeSignal opens one child-process cleanup gate.
func writeSignal(t *testing.T, directory, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
}

// uiHostRequest wraps one Host request for the UI stream.
func uiHostRequest(id string, request *uiv1.HostRequest) *uiv1.OpenRequest {
	return uiv1.OpenRequest_builder{
		OperationId: new(id), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()
}

// receiveUIOperationLifecycle receives Accepted, Running, and Completed for one Host operation.
func receiveUIOperationLifecycle(t *testing.T, stream uiv1.UIService_OpenClient, id string) {
	t.Helper()
	accepted, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, id, accepted.GetOperationId())
	require.NotNil(t, accepted.GetEvent().GetAccepted())
	running, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, id, running.GetOperationId())
	require.NotNil(t, running.GetEvent().GetRunning())
	completed, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, id, completed.GetOperationId())
	require.NotNil(t, completed.GetEvent().GetCompleted().GetInitialized())
}

// receiveUIRequest receives one UI-initiated Host request with the expected identifier.
func receiveUIRequest(t *testing.T, stream uiv1.UIService_OpenClient, id string) *uiv1.OpenResponse {
	t.Helper()
	response, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, id, response.GetOperationId())
	require.NotNil(t, response.GetRequest())
	return response
}

// sendUIEvent sends one lifecycle event for a UI-initiated Host operation.
func sendUIEvent(t *testing.T, stream uiv1.UIService_OpenClient, id string, event *uiv1.HostEvent) {
	t.Helper()
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(id), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
	}.Build()))
}

// acceptedHostEvent creates the Accepted lifecycle event.
func acceptedHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return event
}

// runningHostEvent creates the Running lifecycle event.
func runningHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return event
}

// completedSubmitHostEvent creates the typed Submit terminal event.
func completedSubmitHostEvent() *uiv1.HostEvent {
	completed := new(uiv1.HostCompleted)
	completed.SetSubmit(new(uiv1.SubmitCompleted))
	event := new(uiv1.HostEvent)
	event.SetCompleted(completed)
	return event
}

// completedCancelHostEvent creates the typed cancellation terminal event.
func completedCancelHostEvent() *uiv1.HostEvent {
	completed := new(uiv1.HostCompleted)
	completed.SetCancel(operationv1.CancelCompleted_builder{
		TargetState: new(operationv1.TerminalState_TERMINAL_STATE_CANCELED),
	}.Build())
	event := new(uiv1.HostEvent)
	event.SetCompleted(completed)
	return event
}

// rejectedHostEvent creates the complete public Rejected lifecycle event.
func rejectedHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetRejected(operationv1.Rejected_builder{
		Code: new(invalidArgumentCode), Message: new("complete external UI rejection"),
	}.Build())
	return event
}

// failedHostEvent creates the complete public Failed lifecycle event.
func failedHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetFailed(operationv1.Failed_builder{
		Code: new(internalFailureCode), Message: new("complete external UI failure"),
	}.Build())
	return event
}

// drainUIStream receives until the requested clean EOF.
func drainUIStream(stream uiv1.UIService_OpenClient) error {
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receive UI shutdown response: %w", err)
		}
	}
}
