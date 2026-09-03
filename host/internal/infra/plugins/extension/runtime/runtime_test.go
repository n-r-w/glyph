//go:build integration

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/testsupport/pluginmock"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// runtimeHelperEnvironment selects the child-process fixture behavior.
	runtimeHelperEnvironment = "GLYPH_EXTENSION_RUNTIME_HELPER"
	// runtimeCountEnvironment provides the path used to count remote executions.
	runtimeCountEnvironment = "GLYPH_EXTENSION_RUNTIME_COUNT"
	// runtimeStartedEnvironment provides the path used to signal operation execution.
	runtimeStartedEnvironment = "GLYPH_EXTENSION_RUNTIME_STARTED"
	// runtimeReleaseEnvironment provides the path used to signal admission release.
	runtimeReleaseEnvironment = "GLYPH_EXTENSION_RUNTIME_RELEASE"
	// runtimeReleaseGateEnvironment provides the path that permits admission release to finish.
	runtimeReleaseGateEnvironment = "GLYPH_EXTENSION_RUNTIME_RELEASE_GATE"
	// processOperationTimeout bounds real child-process coordination.
	processOperationTimeout = 10 * time.Second
)

// protocolService prepares selected operations in a real helper process.
type protocolService struct {
	// mode selects the fixture behavior.
	mode string
	// countPath records admitted tool executions when configured.
	countPath string
	// startedPath signals that selected operation work started.
	startedPath string
	// releasePath signals that selected operation release started.
	releasePath string
	// releaseGatePath names the file that permits selected operation release to finish.
	releaseGatePath string
	// attempts counts selected rejection or failure fixture invocations.
	attempts atomic.Int64
}

// protocolRegisterOperation returns one mode-specific registration.
type protocolRegisterOperation struct {
	// service owns the selected fixture mode.
	service *protocolService
}

// protocolHandleOperation returns one observer action.
type protocolHandleOperation struct {
	// service owns the selected fixture mode and release coordination.
	service *protocolService
}

// protocolExecuteOperation runs one mode-specific tool operation.
type protocolExecuteOperation struct {
	// service owns the selected fixture mode and counter path.
	service *protocolService
}

// executionOutcome carries a concurrent execution result back to the test goroutine.
type executionOutcome struct {
	// result contains the synchronous Host tool result.
	result tool.Result
	// err contains the synchronous Host execution error.
	err error
}

// TestFactoryRuntimeSurvivesStartupContextCancellation verifies explicit Host shutdown owns process lifetime.
func TestFactoryRuntimeSurvivesStartupContextCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: create one executable extension candidate and cancelable startup context.
	scriptPath := filepath.Join(t.TempDir(), "glyph-test-extension")
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=default exec %q -test.run=^TestRuntimeHelperProcess$\n",
		runtimeHelperEnvironment,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o700))
	startupContext, cancelStartup := context.WithCancel(t.Context())

	// Act: start the runtime and cancel only its startup context.
	runtime, err := NewFactory().Start(startupContext, extensionruntime.Candidate{
		ID:   "test",
		Path: scriptPath,
	})
	require.NoError(t, err)
	cancelStartup()

	// Assert: keep the process usable until explicit Host shutdown.
	select {
	case <-runtime.Done():
		require.Fail(t, "extension process stopped before explicit Host shutdown")
	case <-time.After(200 * time.Millisecond):
	}
	registration, err := runtime.Register(t.Context())
	require.NoError(t, err)
	assert.Len(t, registration.Tools, 1)

	runtime.Close()
	select {
	case <-runtime.Done():
	case <-time.After(processOperationTimeout):
		require.Fail(t, "extension process did not stop after explicit Host shutdown")
	}
}

// TestRuntimeHelperProcess runs a configurable extension fixture only in a child test process.
func TestRuntimeHelperProcess(t *testing.T) {
	t.Parallel()

	// Arrange: read the isolated child-process mode.
	mode := os.Getenv(runtimeHelperEnvironment)
	if mode == "" {
		return
	}

	// Act: use a direct generated server only for adversarial public-protocol sequences.
	if isAdversarialMode(mode) {
		serveAdversarialExtension(mode)
		return
	}
	fixture := &protocolService{
		mode:            mode,
		countPath:       os.Getenv(runtimeCountEnvironment),
		startedPath:     os.Getenv(runtimeStartedEnvironment),
		releasePath:     os.Getenv(runtimeReleaseEnvironment),
		releaseGatePath: os.Getenv(runtimeReleaseGateEnvironment),
		attempts:        atomic.Int64{},
	}
	extensionsdk.Serve(newProtocolMockService(t, fixture))

	// Assert: go-plugin owns child-process lifetime after the selected server starts.
}

// newProtocolMockService creates generated SDK mocks for one valid child-process fixture.
func newProtocolMockService(t *testing.T, fixture *protocolService) extensionsdk.Service {
	t.Helper()
	controller := gomock.NewController(t)
	service := pluginmock.NewMockExtensionService(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *extensionpb.RegisterRequest) (extensionsdk.RegisterOperation, error) {
			prepared, err := fixture.PrepareRegister(ctx, request)
			if err != nil {
				return nil, err
			}
			registration := pluginmock.NewMockExtensionRegisterOperation(controller)
			registration.EXPECT().Run(gomock.Any()).DoAndReturn(prepared.Run)
			registration.EXPECT().Release().Do(prepared.Release)
			return registration, nil
		},
	).AnyTimes()
	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *extensionpb.HandleRequest) (extensionsdk.HandleOperation, error) {
			prepared, err := fixture.PrepareHandle(ctx, request)
			if err != nil {
				return nil, err
			}
			handler := pluginmock.NewMockExtensionHandleOperation(controller)
			handler.EXPECT().Run(gomock.Any()).DoAndReturn(prepared.Run)
			handler.EXPECT().Release().Do(prepared.Release)
			return handler, nil
		},
	).AnyTimes()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *extensionpb.ExecuteRequest) (extensionsdk.ExecuteOperation, error) {
			prepared, err := fixture.PrepareExecute(ctx, request)
			if err != nil {
				return nil, err
			}
			execution := pluginmock.NewMockExtensionExecuteOperation(controller)
			execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(prepared.Run)
			execution.EXPECT().Release().Do(prepared.Release)
			return execution, nil
		},
	).AnyTimes()
	return service
}

// isAdversarialMode reports whether one helper mode bypasses the SDK to violate the public protocol.
func isAdversarialMode(mode string) bool {
	switch mode {
	case "missing-result", "duplicate-result", "event-after-result", "empty-event",
		"empty-result", "mismatched-handler", "unsupported-rejection", "unsupported-failure",
		"failure-before-accepted", "unknown-operation-rejection", "unknown-operation-failure",
		"cancel-transport-error", "cancel-handle-transport-error",
		"cancel-unknown-transport-error", "cancel-handle-unknown-transport-error":
		return true
	default:
		return false
	}
}

// TestRuntimeWithRealGlyphTools verifies the production process handshake, read descriptor, execution, validation,
// and shutdown.
func TestRuntimeWithRealGlyphTools(t *testing.T) {
	t.Parallel()

	// Arrange: build the production extension and give its process an isolated working project.
	executable := buildGlyphTools(t)
	projectDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDirectory, "notes.txt"), []byte("first\nsecond\n"), 0o600))
	command := exec.CommandContext(t.Context(), executable)
	command.Dir = projectDirectory

	// Act: start the real process and retrieve its complete startup catalog.
	runtime, err := Start(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	registration, err := runtime.Register(t.Context())
	require.NoError(t, err)

	// Assert: expose the complete seven-tool standard catalog.
	require.Len(t, registration.Tools, 7)
	assert.Empty(t, registration.Handlers)
	assert.Equal(t, "read", registration.Tools[0].Name)
	assert.NotEmpty(t, registration.Tools[0].Description)
	assert.NotEmpty(t, registration.Tools[0].InputSchemaJSON)

	// Act: read a relative project file through the real Execute operation.
	result, err := runtime.Execute(
		t.Context(),
		"read",
		[]byte(`{"path":"notes.txt"}`),
		discardProgress,
	)

	// Assert: preserve complete text in exactly one terminal successful result.
	require.NoError(t, err)
	assert.Equal(t, tool.Result{
		Contents: tool.TextContents("first\nsecond\n"),
		IsError:  false,
	}, result)

	// Act: replace one unique fragment through the production edit tool.
	editResult, err := runtime.Execute(
		t.Context(),
		"edit",
		[]byte(`{"path":"notes.txt","edits":[{"oldText":"first","newText":"updated"}]}`),
		discardProgress,
	)
	require.NoError(t, err)
	assert.False(t, editResult.IsError)
	editedContent, err := os.ReadFile(filepath.Join(projectDirectory, "notes.txt"))
	require.NoError(t, err)
	assert.Equal(t, "updated\nsecond\n", string(editedContent))

	// Act: stream both output channels and return a nonzero terminal bash result.
	bashProgress := make([]tool.ProgressChannel, 0, 3)
	bashFragments := make([]string, 0, 2)
	bashResult, err := runtime.Execute(
		t.Context(),
		"bash",
		[]byte(`{"command":"printf out; printf err >&2; exit 7"}`),
		func(progress tool.Progress) error {
			bashProgress = append(bashProgress, progress.Channel)
			if progress.Channel == tool.ProgressChannelStdout || progress.Channel == tool.ProgressChannelStderr {
				bashFragments = append(bashFragments, progress.Content)
			}
			return nil
		},
	)
	require.NoError(t, err)
	assert.True(t, bashResult.IsError)
	assert.Equal(t, strings.Join(bashFragments, "")+"\n\n[Exit code: 7]\n", bashResult.Contents[0].Text.OrEmpty())
	assert.Contains(t, bashProgress, tool.ProgressChannelStatus)
	assert.Contains(t, bashProgress, tool.ProgressChannelStdout)
	assert.Contains(t, bashProgress, tool.ProgressChannelStderr)

	// Act: submit arguments outside the cached descriptor schema.
	invalidResult, err := runtime.Execute(t.Context(), "read", []byte(`{}`), discardProgress)

	// Assert: reject them as a terminal tool error without making the process unavailable.
	require.NoError(t, err)
	assert.True(t, invalidResult.IsError)
	assert.NotEmpty(t, invalidResult.Contents[0].Text)
	assertRuntimeRunning(t, runtime)

	// Arrange: a long-running bundled bash process stays active until its operation context is canceled.
	ctx, cancel := context.WithCancel(t.Context())
	executionChannel := make(chan executionOutcome, 1)
	started := make(chan struct{})
	go func() {
		executionResult, executionErr := runtime.Execute(
			ctx,
			"bash",
			[]byte(`{"command":"printf started; while :; do sleep 1; done"}`),
			func(progress tool.Progress) error {
				if progress.Channel == tool.ProgressChannelStdout && strings.Contains(progress.Content, "started") {
					close(started)
				}
				return nil
			},
		)
		executionChannel <- executionOutcome{result: executionResult, err: executionErr}
	}()

	// Act: wait for real process output before canceling the bundled bash operation.
	select {
	case <-started:
	case <-time.After(processOperationTimeout):
		require.FailNow(t, "glyph-tools did not start the blocking bash process")
	}
	cancel()

	// Assert: active cancellation joins the real bundled process without stopping the extension runtime.
	select {
	case execution := <-executionChannel:
		assert.Equal(t, tool.Result{Contents: nil, IsError: false}, execution.result)
		require.ErrorIs(t, execution.err, context.Canceled)
	case <-time.After(processOperationTimeout):
		require.FailNow(t, "glyph-tools did not stop the blocking bash process after cancellation")
	}
	assertRuntimeRunning(t, runtime)

	// Act: stop the extension process through the Host runtime adapter.
	runtime.Close()

	// Assert: shutdown waits until the process has exited.
	requireRuntimeStopped(t, runtime)
}

// TestMapResultContentsPreservesOrderedTextAndImage verifies exact typed transport mapping.
func TestMapResultContentsPreservesOrderedTextAndImage(t *testing.T) {
	t.Parallel()

	// Arrange: create ordered text, image, and text result blocks.
	source := []*extensionpb.ToolResultContent{
		//nolint:exhaustruct_v5 // extensionpb.ToolResultContent_builder sets only the active Text field.
		extensionpb.ToolResultContent_builder{
			Text: new("first"),
		}.Build(),
		//nolint:exhaustruct_v5 // extensionpb.ToolResultContent_builder sets only the active Image field.
		extensionpb.ToolResultContent_builder{
			Image: extensionpb.ToolResultImage_builder{
				MediaType: new("image/png"),
				Data:      []byte{0, 1, 2, 3},
			}.Build(),
		}.Build(),
		//nolint:exhaustruct_v5 // extensionpb.ToolResultContent_builder sets only the active Text field.
		extensionpb.ToolResultContent_builder{
			Text: new("last"),
		}.Build(),
	}

	// Act: map the protobuf result blocks.
	contents, err := mapResultContents(source)

	// Assert: preserve block order, kinds, and payloads.
	require.NoError(t, err)
	assert.Equal(t, []tool.ResultContent{
		{
			Kind:  tool.ResultContentText,
			Text:  mo.Some("first"),
			Image: mo.None[tool.ResultImage](),
		},
		{
			Kind: tool.ResultContentImage,
			Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{
				MediaType: "image/png",
				Data:      []byte{0, 1, 2, 3},
			}),
		},
		{
			Kind:  tool.ResultContentText,
			Text:  mo.Some("last"),
			Image: mo.None[tool.ResultImage](),
		},
	}, contents)
}

// TestMapResultContentsRejectsEmptyResult prevents invalid empty provider output lists.
func TestMapResultContentsRejectsEmptyResult(t *testing.T) {
	t.Parallel()

	// Arrange: use an empty terminal content list.
	var source []*extensionpb.ToolResultContent

	// Act: map the empty result.
	_, err := mapResultContents(source)

	// Assert: reject missing model-visible result data.
	require.ErrorContains(t, err, "result contents are empty")
}

// TestMapResultContentsRejectsEmptyImageData prevents invalid provider image payloads.
func TestMapResultContentsRejectsEmptyImageData(t *testing.T) {
	t.Parallel()

	// Arrange: create one image result without encoded bytes.
	source := []*extensionpb.ToolResultContent{
		//nolint:exhaustruct_v5 // extensionpb.ToolResultContent_builder sets only the active Image field.
		extensionpb.ToolResultContent_builder{
			Image: extensionpb.ToolResultImage_builder{
				MediaType: new("image/png"),
				Data:      nil,
			}.Build(),
		}.Build(),
	}

	// Act: map the invalid image result.
	_, err := mapResultContents(source)

	// Assert: reject the empty image payload.
	require.ErrorContains(t, err, "result image 0 is invalid")
}

// TestRuntimePropagatesActiveCancellation verifies cancellation of an active Execute operation.
func TestRuntimePropagatesActiveCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: start a helper process that reports readiness and then waits for stream cancellation.
	runtime := startHelperRuntime(t, "wait")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	outcome := make(chan executionOutcome, 1)

	// Act: execute concurrently so cancellation occurs only after server progress reaches the Host.
	go func() {
		result, executeErr := runtime.Execute(ctx, "read", []byte(`{"path":"notes.txt"}`), func(tool.Progress) error {
			close(started)
			return nil
		})
		outcome <- executionOutcome{
			result: result,
			err:    executeErr,
		}
	}()
	<-started
	cancel()
	execution := <-outcome

	// Assert: cancellation remains identifiable and does not become a protocol violation.
	require.ErrorIs(t, execution.err, context.Canceled)
	assert.Equal(t, tool.Result{
		Contents: nil,
		IsError:  false,
	}, execution.result)
	assertRuntimeRunning(t, runtime)
}

// TestRuntimeExecuteCancellationWaitsForRelease verifies synchronous Execute joins remote cleanup.
func TestRuntimeExecuteCancellationWaitsForRelease(t *testing.T) {
	t.Parallel()

	// Arrange: block the remote operation release after its work observes cancellation.
	coordinationDir := t.TempDir()
	releasePath := filepath.Join(coordinationDir, "release-started")
	releaseGatePath := filepath.Join(coordinationDir, "release-gate")
	runtime := startReleaseGatedHelperRuntime(t, "wait-release", "", releasePath, releaseGatePath)
	t.Cleanup(func() { _ = os.WriteFile(releaseGatePath, nil, 0o600) })
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	outcome := make(chan executionOutcome, 1)
	go func() {
		result, executeErr := runtime.Execute(ctx, "read", []byte(`{"path":"notes.txt"}`), func(tool.Progress) error {
			close(started)
			return nil
		})
		outcome <- executionOutcome{result: result, err: executeErr}
	}()
	<-started

	// Act: cancel after Run starts and wait until Release is blocked by the fixture gate.
	cancel()
	require.Eventually(t, func() bool { return pathExists(releasePath) }, processOperationTimeout, 10*time.Millisecond)

	// Assert: Execute cannot return until target Release finishes.
	select {
	case execution := <-outcome:
		require.FailNowf(t, "Execute returned before target release finished", "error: %v", execution.err)
	default:
	}
	require.NoError(t, os.WriteFile(releaseGatePath, nil, 0o600))
	execution := <-outcome
	require.ErrorIs(t, execution.err, context.Canceled)
}

// TestRuntimeHandleCancellationWaitsForRelease verifies synchronous Handle joins remote cleanup.
func TestRuntimeHandleCancellationWaitsForRelease(t *testing.T) {
	t.Parallel()

	// Arrange: expose a handler whose Release remains blocked after cancellation.
	coordinationDir := t.TempDir()
	startedPath := filepath.Join(coordinationDir, "run-started")
	releasePath := filepath.Join(coordinationDir, "release-started")
	releaseGatePath := filepath.Join(coordinationDir, "release-gate")
	runtime := startReleaseGatedHelperRuntime(
		t,
		"wait-handle-release",
		startedPath,
		releasePath,
		releaseGatePath,
	)
	t.Cleanup(func() { _ = os.WriteFile(releaseGatePath, nil, 0o600) })
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	outcome := make(chan error, 1)
	go func() {
		_, handleErr := runtime.Handle(ctx, "observer", sessiontree.HandlerRequest{
			Request: mo.None[sessiontree.RequestHandlerInvocation](),
			Result:  mo.None[sessiontree.ResultHandlerInvocation](),
			Observer: mo.Some(sessiontree.TreeObserverInvocation{
				SessionID:               "session",
				TargetEntryID:           "target",
				PrecedingActiveLeafID:   mo.None[string](),
				NavigationDestinationID: mo.None[string](),
				CommittedActiveLeafID:   mo.None[string](),
				CreatedSummary:          mo.None[session.Entry](),
			}),
		})
		outcome <- handleErr
	}()
	require.Eventually(t, func() bool { return pathExists(startedPath) }, processOperationTimeout, 10*time.Millisecond)

	// Act: cancel after Run starts and wait until Release is blocked by the fixture gate.
	cancel()
	require.Eventually(t, func() bool { return pathExists(releasePath) }, processOperationTimeout, 10*time.Millisecond)

	// Assert: Handle cannot return until target Release finishes.
	select {
	case handleErr := <-outcome:
		require.FailNowf(t, "Handle returned before target release finished", "error: %v", handleErr)
	default:
	}
	require.NoError(t, os.WriteFile(releaseGatePath, nil, 0o600))
	require.ErrorIs(t, <-outcome, context.Canceled)
}

// TestRuntimeCancellationPreservesPrimaryAndTransportErrors verifies independent causes survive synchronous joining.
func TestRuntimeCancellationPreservesPrimaryAndTransportErrors(t *testing.T) {
	t.Parallel()

	// Arrange: use a peer that drops the stream after it receives the cancellation request.
	runtime := startHelperRuntime(t, "cancel-transport-error")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())

	// Act: cancel from the first progress callback so the target and transport errors race.
	_, err = runtime.Execute(ctx, "read", []byte(`{"path":"notes.txt"}`), func(tool.Progress) error {
		cancel()
		return nil
	})

	// Assert: preserve both causes and finish failed-process cleanup before Execute returns.
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	require.ErrorContains(t, err, "cancellation transport failed")
	select {
	case <-runtime.Done():
	default:
		require.FailNow(t, "Execute returned before failed runtime process cleanup")
	}
}

// TestRuntimeExecuteCancellationPreservesUnknownTransportFailure verifies explicit Unknown remains connection-fatal.
func TestRuntimeExecuteCancellationPreservesUnknownTransportFailure(t *testing.T) {
	t.Parallel()

	// Arrange: use a peer that returns explicit gRPC Unknown after it receives cancellation.
	runtime := startHelperRuntime(t, "cancel-unknown-transport-error")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())

	// Act: cancel from progress while the peer fails the cancellation transport.
	_, err = runtime.Execute(ctx, "read", []byte(`{"path":"notes.txt"}`), func(tool.Progress) error {
		cancel()
		return nil
	})

	// Assert: preserve caller and gRPC causes and finish process cleanup before return.
	require.ErrorIs(t, err, context.Canceled)
	transportStatus, hasStatus := status.FromError(err)
	require.True(t, hasStatus)
	assert.Equal(t, codes.Unknown, transportStatus.Code())
	require.ErrorContains(t, err, "unknown cancellation transport failed")
	select {
	case <-runtime.Done():
	default:
		require.FailNow(t, "Execute returned before Unknown-failed runtime process cleanup")
	}
}

// TestRuntimeHandleCancellationPreservesTransportFailure verifies independent Handler failure cleanup.
func TestRuntimeHandleCancellationPreservesTransportFailure(t *testing.T) {
	t.Parallel()

	// Arrange: start a handler peer that reports transport loss after receiving cancellation.
	startedPath := filepath.Join(t.TempDir(), "handle-started")
	runtime := startReleaseGatedHelperRuntime(t, "cancel-handle-transport-error", startedPath, "", "")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	outcome := make(chan error, 1)
	go func() {
		_, handleErr := runtime.Handle(ctx, "observer", sessiontree.HandlerRequest{
			Request: mo.None[sessiontree.RequestHandlerInvocation](),
			Result:  mo.None[sessiontree.ResultHandlerInvocation](),
			Observer: mo.Some(sessiontree.TreeObserverInvocation{
				SessionID:               "session",
				TargetEntryID:           "target",
				PrecedingActiveLeafID:   mo.None[string](),
				NavigationDestinationID: mo.None[string](),
				CommittedActiveLeafID:   mo.None[string](),
				CreatedSummary:          mo.None[session.Entry](),
			}),
		})
		outcome <- handleErr
	}()
	require.Eventually(t, func() bool { return pathExists(startedPath) }, processOperationTimeout, 10*time.Millisecond)

	// Act: cancel after the peer accepted and started the Handle operation.
	cancel()
	var handleErr error
	select {
	case handleErr = <-outcome:
	case <-time.After(processOperationTimeout):
		require.FailNow(t, "Handle did not settle after cancellation transport failure")
	}

	// Assert: preserve both causes and finish failed-process cleanup before Handle returns.
	require.ErrorIs(t, handleErr, context.Canceled)
	assert.Equal(t, codes.Unavailable, status.Code(handleErr))
	require.ErrorContains(t, handleErr, "cancellation transport failed")
	select {
	case <-runtime.Done():
	default:
		require.FailNow(t, "Handle returned before failed runtime process cleanup")
	}
}

// TestRuntimeHandleCancellationPreservesUnknownTransportFailure verifies explicit Unknown cleanup for Handle.
func TestRuntimeHandleCancellationPreservesUnknownTransportFailure(t *testing.T) {
	t.Parallel()

	// Arrange: start a handler peer that returns explicit gRPC Unknown after cancellation.
	startedPath := filepath.Join(t.TempDir(), "handle-started")
	runtime := startReleaseGatedHelperRuntime(
		t,
		"cancel-handle-unknown-transport-error",
		startedPath,
		"",
		"",
	)
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	outcome := make(chan error, 1)
	go func() {
		_, handleErr := runtime.Handle(ctx, "observer", sessiontree.HandlerRequest{
			Request: mo.None[sessiontree.RequestHandlerInvocation](),
			Result:  mo.None[sessiontree.ResultHandlerInvocation](),
			Observer: mo.Some(sessiontree.TreeObserverInvocation{
				SessionID:               "session",
				TargetEntryID:           "target",
				PrecedingActiveLeafID:   mo.None[string](),
				NavigationDestinationID: mo.None[string](),
				CommittedActiveLeafID:   mo.None[string](),
				CreatedSummary:          mo.None[session.Entry](),
			}),
		})
		outcome <- handleErr
	}()
	require.Eventually(t, func() bool { return pathExists(startedPath) }, processOperationTimeout, 10*time.Millisecond)

	// Act: cancel after the peer accepted and started Handle.
	cancel()
	var handleErr error
	select {
	case handleErr = <-outcome:
	case <-time.After(processOperationTimeout):
		require.FailNow(t, "Handle did not settle after Unknown cancellation transport failure")
	}

	// Assert: preserve caller and gRPC causes and finish process cleanup before return.
	require.ErrorIs(t, handleErr, context.Canceled)
	transportStatus, hasStatus := status.FromError(handleErr)
	require.True(t, hasStatus)
	assert.Equal(t, codes.Unknown, transportStatus.Code())
	require.ErrorContains(t, handleErr, "unknown cancellation transport failed")
	select {
	case <-runtime.Done():
	default:
		require.FailNow(t, "Handle returned before Unknown-failed runtime process cleanup")
	}
}

// TestRuntimeCloseWaitsForActiveRelease verifies requested Host closure joins remote operation cleanup.
func TestRuntimeCloseWaitsForActiveRelease(t *testing.T) {
	t.Parallel()

	// Arrange: run one remote operation whose Release waits for a parent-controlled gate.
	coordinationDir := t.TempDir()
	releasePath := filepath.Join(coordinationDir, "release-started")
	releaseGatePath := filepath.Join(coordinationDir, "release-gate")
	runtime := startReleaseGatedHelperRuntime(t, "wait-release", "", releasePath, releaseGatePath)
	t.Cleanup(func() { _ = os.WriteFile(releaseGatePath, nil, 0o600) })
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	started := make(chan struct{})
	executeOutcome := make(chan error, 1)
	go func() {
		_, executeErr := runtime.Execute(
			t.Context(),
			"read",
			[]byte(`{"path":"notes.txt"}`),
			func(tool.Progress) error {
				close(started)
				return nil
			},
		)
		executeOutcome <- executeErr
	}()
	<-started
	closeDone := make(chan struct{})

	// Act: request closure while Execute remains active.
	go func() {
		runtime.Close()
		close(closeDone)
	}()
	require.Eventually(t, func() bool { return pathExists(releasePath) }, processOperationTimeout, 10*time.Millisecond)

	// Assert: close and the synchronous operation remain joined until Release finishes.
	select {
	case <-closeDone:
		require.FailNow(t, "Runtime.Close returned before active release finished")
	default:
	}
	select {
	case <-executeOutcome:
		require.FailNow(t, "Execute returned before close joined active release")
	default:
	}
	require.NoError(t, os.WriteFile(releaseGatePath, nil, 0o600))
	<-closeDone
	require.Error(t, <-executeOutcome)
}

// TestRuntimeRejectsExecutionProtocolViolations verifies every terminal-stream invariant from Extension Contract v1.
func TestRuntimeRejectsExecutionProtocolViolations(t *testing.T) {
	t.Parallel()

	testCases := []string{"missing-result", "duplicate-result", "event-after-result", "empty-event"}
	for _, mode := range testCases {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			// Arrange: start a real helper process with one deliberately malformed stream behavior.
			runtime := startHelperRuntime(t, mode)
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)

			// Act: consume the malformed lifecycle sequence.
			result, err := runtime.Execute(
				t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
			)
			if (mode == "duplicate-result" || mode == "event-after-result") && err == nil {
				// A valid terminal event can reach its caller before the later connection violation is observed.
				require.Len(t, result.Contents, 1)
				text, present := result.Contents[0].Text.Get()
				assert.True(t, present)
				assert.Equal(t, "done", text)
				result, err = runtime.Execute(
					t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
				)
			}

			// Assert: reject the violating connection and expose the exact protocol cause.
			assert.Equal(t, tool.Result{Contents: nil, IsError: false}, result)
			require.Error(t, err)
			require.ErrorIs(t, err, extensionruntime.ErrExtensionUnavailable)
			require.ErrorContains(t, err, "extension protocol violation")
			if mode == "duplicate-result" {
				require.ErrorContains(t, err, "completed event")
			}
			if mode == "event-after-result" {
				require.ErrorContains(t, err, "progress event")
			}
			requireRuntimeStopped(t, runtime)
		})
	}
}

// TestRuntimeRejectsMalformedCompletedPayloads verifies Host payload validation fails the shared connection.
func TestRuntimeRejectsMalformedCompletedPayloads(t *testing.T) {
	t.Parallel()

	// Arrange: define malformed Execute and Handle completion scenarios.
	testCases := map[string]func(*testing.T, *Runtime) error{
		"empty Execute result": func(t *testing.T, runtime *Runtime) error {
			_, err := runtime.Execute(
				t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
			)
			return err
		},
		"mismatched Handle action": func(t *testing.T, runtime *Runtime) error {
			request := sessiontree.HandlerRequest{
				Request: mo.None[sessiontree.RequestHandlerInvocation](),
				Result:  mo.None[sessiontree.ResultHandlerInvocation](),
				Observer: mo.Some(sessiontree.TreeObserverInvocation{
					SessionID: "session", TargetEntryID: "target",
					PrecedingActiveLeafID: mo.None[string](), NavigationDestinationID: mo.None[string](),
					CommittedActiveLeafID: mo.None[string](), CreatedSummary: mo.None[session.Entry](),
				}),
			}
			_, err := runtime.Handle(t.Context(), "observer", request)
			return err
		},
	}
	modes := map[string]string{
		"empty Execute result":     "empty-result",
		"mismatched Handle action": "mismatched-handler",
	}
	for name, invoke := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := startHelperRuntime(t, modes[name])
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)

			// Act: consume the malformed completed payload.
			err = invoke(t, runtime)

			// Assert: fail and join the connection without waiting for peer EOF.
			require.Error(t, err)
			require.ErrorIs(t, err, extensionruntime.ErrExtensionUnavailable)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			require.ErrorContains(t, err, "extension protocol violation")
			requireRuntimeStopped(t, runtime)
		})
	}
}

// TestRuntimeKeepsHandlerErrorsAsCompletedData verifies ordinary handler errors do not stop the runtime.
func TestRuntimeKeepsHandlerErrorsAsCompletedData(t *testing.T) {
	t.Parallel()

	// Arrange: start an SDK extension that completes Handle with HandlerError data.
	runtime := startHelperRuntime(t, "handler-error")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	request := validSessionTreeHandlerRequest()

	// Act: invoke the handler twice through the local gRPC boundary.
	_, firstErr := runtime.Handle(t.Context(), "observer", request)
	_, secondErr := runtime.Handle(t.Context(), "observer", request)

	// Assert: return complete ordinary data errors while the runtime remains available.
	for _, handleErr := range []error{firstErr, secondErr} {
		require.EqualError(t, handleErr, "complete handler error text")
		require.NotErrorIs(t, handleErr, extensionruntime.ErrExtensionUnavailable)
	}
	select {
	case <-runtime.Done():
		assert.Fail(t, "runtime stopped after completed HandlerError data")
	default:
	}
}

// TestRuntimeRejectsPeerErrorLifecycleViolations verifies peer error payloads survive later protocol validation.
func TestRuntimeRejectsPeerErrorLifecycleViolations(t *testing.T) {
	t.Parallel()

	// Arrange: define valid peer errors that violate lifecycle or operation ownership.
	testCases := map[string]struct {
		mode          string
		category      string
		message       string
		localFragment string
		peerContext   string
	}{
		"Failed before Accepted": {
			mode: "failure-before-accepted", category: "INTERNAL",
			message: "complete failure before Accepted text", localFragment: "cannot precede Accepted",
			peerContext: `peer failure category "INTERNAL"`,
		},
		"Rejected for unknown operation": {
			mode: "unknown-operation-rejection", category: "BUSY",
			message: "complete unknown operation rejection text", localFragment: `unknown operation "unknown"`,
			peerContext: `peer rejection category "BUSY"`,
		},
		"Failed for unknown operation": {
			mode: "unknown-operation-failure", category: "INTERNAL",
			message: "complete unknown operation failure text", localFragment: `unknown operation "unknown"`,
			peerContext: `peer failure category "INTERNAL"`,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := startHelperRuntime(t, testCase.mode)
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)

			// Act: execute work against the peer lifecycle violation.
			_, err = runtime.Execute(
				t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
			)

			// Assert: retain all local and peer context once, classify unavailability, and stop the runtime.
			require.Error(t, err)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			require.ErrorIs(t, err, extensionruntime.ErrExtensionUnavailable)
			require.ErrorContains(t, err, `execute extension tool "read"`)
			require.ErrorContains(t, err, "extension protocol violation")
			require.ErrorContains(t, err, testCase.localFragment)
			require.ErrorContains(t, err, testCase.peerContext)
			require.ErrorContains(t, err, testCase.message)
			assert.Equal(t, 1, strings.Count(err.Error(), testCase.category))
			assert.Equal(t, 1, strings.Count(err.Error(), testCase.message))
			requireRuntimeStopped(t, runtime)
		})
	}
}

// TestRuntimeRejectsUnsupportedPeerCategories verifies invalid peer categories retain text and stop the runtime.
func TestRuntimeRejectsUnsupportedPeerCategories(t *testing.T) {
	t.Parallel()

	// Arrange: define direct peers and every required local and peer error context layer.
	testCases := map[string]struct {
		mode      string
		message   string
		fragments []string
	}{
		"rejection": {
			mode: "unsupported-rejection", message: "complete peer rejection text",
			fragments: []string{
				`execute extension tool "read"`, "extension protocol violation",
				`unsupported extension rejection code "UNSUPPORTED"`, "for request kind 4",
				"peer rejection text",
			},
		},
		"failure": {
			mode: "unsupported-failure", message: "complete peer failure text",
			fragments: []string{
				`execute extension tool "read"`, "extension protocol violation",
				`unsupported extension failure code "UNSUPPORTED"`, "peer failure text",
			},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := startHelperRuntime(t, testCase.mode)
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)

			// Act: execute work against the malformed direct peer.
			_, err = runtime.Execute(
				t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
			)

			// Assert: preserve every context layer once, then stop the unavailable runtime.
			require.Error(t, err)
			require.ErrorIs(t, err, extensionruntime.ErrExtensionUnavailable)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			for _, fragment := range testCase.fragments {
				require.ErrorContains(t, err, fragment)
			}
			require.ErrorContains(t, err, testCase.message)
			assert.Equal(t, 1, strings.Count(err.Error(), "UNSUPPORTED"), name)
			assert.Equal(t, 1, strings.Count(err.Error(), testCase.message), name)
			requireRuntimeStopped(t, runtime)
		})
	}
}

// TestRuntimeProgressDeliveryFailurePreservesProcess keeps a healthy extension available.
func TestRuntimeProgressDeliveryFailurePreservesProcess(t *testing.T) {
	t.Parallel()

	// Arrange: start a healthy extension that reports progress.
	runtime := startHelperRuntime(t, "progress")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	deliveryErr := errors.New("event consumer failed")

	// Act: fail the Host progress callback.
	_, err = runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), func(tool.Progress) error {
		return deliveryErr
	})
	// Assert: preserve the callback cause and keep later execution available.
	require.ErrorIs(t, err, deliveryErr)
	require.NotErrorIs(t, err, extensionruntime.ErrExtensionUnavailable)
	assertRuntimeRunning(t, runtime)

	result, err := runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress)
	require.NoError(t, err)
	assert.Equal(t, tool.Result{
		Contents: tool.TextContents("done"),
		IsError:  false,
	}, result)
}

// TestRuntimeClassifiesTransportFailure marks the closed runtime unavailable.
func TestRuntimeClassifiesTransportFailure(t *testing.T) {
	t.Parallel()

	// Arrange: start an extension that exits during Execute.
	runtime := startHelperRuntime(t, "transport-error")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)

	// Act: execute work across the failing transport.
	_, err = runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress)

	// Assert: classify unavailability and stop the failed runtime.
	require.ErrorIs(t, err, extensionruntime.ErrExtensionUnavailable)
	requireRuntimeStopped(t, runtime)
}

// TestRuntimeForwardsProgress verifies ordered progress delivery before the terminal result.
func TestRuntimeForwardsProgress(t *testing.T) {
	t.Parallel()

	// Arrange: start a helper process that emits one status fragment and one result.
	runtime := startHelperRuntime(t, "progress")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)
	progress := make([]tool.Progress, 0, 1)

	// Act: wait for the valid Execute operation and collect progress.
	result, err := runtime.Execute(
		t.Context(),
		"read",
		[]byte(`{"path":"notes.txt"}`),
		func(event tool.Progress) error {
			progress = append(progress, event)
			return nil
		},
	)

	// Assert: deliver progress before returning the one terminal result.
	require.NoError(t, err)
	assert.Equal(t, []tool.Progress{{
		Channel: tool.ProgressChannelStatus,
		Content: "working",
	}}, progress)
	assert.Equal(t, tool.Result{
		Contents: tool.TextContents("done"),
		IsError:  false,
	}, result)
}

// TestRuntimeHandleInvokesSessionTreeObserverOperation verifies typed observer dispatch over the operation stream.
func TestRuntimeHandleInvokesSessionTreeObserverOperation(t *testing.T) {
	t.Parallel()

	// Arrange: start a real helper extension with one registered observer.
	runtime := startHelperRuntime(t, "handler")
	registration, err := runtime.Register(t.Context())
	require.NoError(t, err)
	require.Equal(t, []startup.RawHandlerDescriptor{{
		Present: true, ID: "observer", Kind: startup.RawHandlerKindSessionTree,
	}}, registration.Handlers)
	invocation := sessiontree.TreeObserverInvocation{
		SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
		NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
	}
	request := sessiontree.HandlerRequest{
		Request:  mo.None[sessiontree.RequestHandlerInvocation](),
		Result:   mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.Some(invocation),
	}

	// Act: invoke the registered handler operation.
	response, err := runtime.Handle(t.Context(), "observer", request)

	// Assert: preserve the typed observer acknowledgement.
	require.NoError(t, err)
	assert.Equal(t, sessiontree.HandlerResponse{
		Request:  mo.None[sessiontree.RequestHandlerAction](),
		Result:   mo.None[sessiontree.ResultHandlerAction](),
		Observer: mo.Some(sessiontree.ObserverAction{}),
	}, response)
}

// PrepareRegister admits one mode-specific registration operation.
func (s *protocolService) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	if s.mode == "register-rejection" {
		return nil, extensionsdk.Reject("INVALID_ARGUMENT", errors.New("complete Register rejection source"))
	}
	return &protocolRegisterOperation{service: s}, nil
}

// PrepareHandle validates and admits the fixture observer invocation.
func (s *protocolService) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	if (s.mode != "handler" && s.mode != "handler-error" && s.mode != "handle-rejection" &&
		s.mode != "handle-failure" && s.mode != "wait-handle-release") ||
		request.GetHandlerId() != "observer" || request.GetSessionTree() == nil {
		return nil, extensionsdk.Reject("INVALID_ARGUMENT", errors.New("unexpected handler request"))
	}
	if request.GetSessionTree().GetSessionId() != "session" || request.GetSessionTree().GetTargetEntryId() != "target" {
		return nil, extensionsdk.Reject("INVALID_ARGUMENT", errors.New("unexpected observer payload"))
	}
	if s.mode == "handle-rejection" && s.attempts.Add(1) == 1 {
		return nil, extensionsdk.Reject("BUSY", errors.New("complete Handle rejection source"))
	}
	return &protocolHandleOperation{service: s}, nil
}

// PrepareExecute records and admits one fixture tool execution.
func (s *protocolService) PrepareExecute(
	context.Context,
	*extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if s.mode == "execute-rejection" && s.attempts.Add(1) == 1 {
		return nil, extensionsdk.Reject("BUSY", errors.New("complete Execute rejection source"))
	}
	if s.countPath != "" {
		file, err := os.OpenFile(s.countPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		if _, err = file.WriteString("1\n"); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
	}
	return &protocolExecuteOperation{service: s}, nil
}

// Run returns a valid or deliberately invalid catalog selected by the helper mode.
func (operation *protocolRegisterOperation) Run(
	context.Context,
) (*extensionpb.RegisterResponse, error) {
	if operation.service.mode == "register-failure" {
		return nil, extensionsdk.Fail("INTERNAL", errors.New("complete Register failure source"))
	}
	descriptor := extensionpb.ToolDescriptor_builder{
		Name: new("read"), Description: new("Read a project file."),
		InputSchemaJson: []byte(validSchemaJSON), ConstrainedSampling: nil,
	}.Build()
	response := extensionpb.RegisterResponse_builder{
		Tools: []*extensionpb.ToolDescriptor{descriptor}, Handlers: nil,
	}.Build()

	switch operation.service.mode {
	case "empty-name":
		descriptor.SetName("")
	case "empty-description":
		descriptor.SetDescription("")
	case "invalid-schema-json":
		descriptor.SetInputSchemaJson([]byte(`{"type":`))
	case "duplicate-name":
		response.SetTools(append(response.GetTools(), descriptor))
	case "handler", "handler-error", "handle-rejection", "handle-failure", "wait-handle-release":
		response.SetHandlers([]*extensionpb.HandlerDescriptor{
			extensionpb.HandlerDescriptor_builder{
				Id: new("observer"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
			}.Build(),
		})
	}
	return response, nil
}

// Release has no fixture registration reservation to free.
func (operation *protocolRegisterOperation) Release() {}

// Run returns the typed observer acknowledgement.
func (operation *protocolHandleOperation) Run(
	ctx context.Context,
) (*extensionpb.HandleResponse, error) {
	if operation.service.mode == "wait-handle-release" {
		writeSignalFile(operation.service.startedPath)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if operation.service.mode == "handle-failure" && operation.service.attempts.Add(1) == 1 {
		return nil, extensionsdk.Fail("INTERNAL", errors.New("complete Handle failure source"))
	}
	if operation.service.mode == "handler-error" {
		return extensionpb.HandleResponse_builder{
			SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, SessionTree: nil,
			Error: extensionpb.HandlerError_builder{Message: new("complete handler error text")}.Build(),
		}.Build(), nil
	}
	//nolint:exhaustruct_v5 // The response builder sets only the observer action.
	return extensionpb.HandleResponse_builder{
		SessionTree: extensionpb.SessionTreeAction_builder{}.Build(),
	}.Build(), nil
}

// Release frees the fixture handler admission.
func (operation *protocolHandleOperation) Release() {
	operation.service.waitForReleaseGate()
}

// Run emits the selected fixture tool behavior.
func (operation *protocolExecuteOperation) Run(
	ctx context.Context,
	reporter *extensionsdk.ProgressReporter,
) (*extensionpb.ToolResult, error) {
	progress := extensionpb.ToolProgress_builder{
		Channel: new(extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS), Content: new("working"),
	}.Build()
	result := extensionpb.ToolResult_builder{
		Contents: []*extensionpb.ToolResultContent{
			//nolint:exhaustruct_v5 // The content builder sets only text.
			extensionpb.ToolResultContent_builder{Text: new("done")}.Build(),
		},
		IsError: new(false),
	}.Build()

	switch operation.service.mode {
	case "execute-failure":
		if operation.service.attempts.Add(1) == 1 {
			return nil, extensionsdk.Fail("INTERNAL", errors.New("complete Execute failure source"))
		}
		if err := reporter.Report(ctx, progress); err != nil {
			return nil, err
		}
		return result, nil
	case "wait", "wait-release":
		if err := reporter.Report(ctx, progress); err != nil {
			return nil, err
		}
		<-ctx.Done()
		return nil, ctx.Err()
	case "transport-error":
		os.Exit(2)
		return nil, errors.New("extension process did not exit")
	default:
		if err := reporter.Report(ctx, progress); err != nil {
			return nil, err
		}
		return result, nil
	}
}

// Release frees the fixture execution admission.
func (operation *protocolExecuteOperation) Release() {
	operation.service.waitForReleaseGate()
}

// startHelperRuntime starts this test binary as a real extension process.
func startHelperRuntime(t *testing.T, mode string) *Runtime {
	t.Helper()

	return startReleaseGatedHelperRuntime(t, mode, "", "", "")
}

// startReleaseGatedHelperRuntime starts one child fixture with optional file coordination.
func startReleaseGatedHelperRuntime(
	t *testing.T,
	mode string,
	startedPath string,
	releasePath string,
	releaseGatePath string,
) *Runtime {
	t.Helper()

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeHelperProcess$")
	command.Env = append(
		os.Environ(),
		runtimeHelperEnvironment+"="+mode,
		runtimeStartedEnvironment+"="+startedPath,
		runtimeReleaseEnvironment+"="+releasePath,
		runtimeReleaseGateEnvironment+"="+releaseGatePath,
	)
	runtime, err := Start(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	return runtime
}

// waitForReleaseGate signals Release entry and waits until the parent permits completion.
func (s *protocolService) waitForReleaseGate() {
	if s.releaseGatePath == "" {
		return
	}
	writeSignalFile(s.releasePath)
	for !pathExists(s.releaseGatePath) {
		time.Sleep(10 * time.Millisecond)
	}
}

// writeSignalFile creates one empty child-process coordination file.
func writeSignalFile(path string) {
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		panic(fmt.Errorf("write coordination file %q: %w", path, err))
	}
}

// pathExists reports whether one coordination path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buildGlyphTools builds the production extension executable for the real-process test.
func buildGlyphTools(t *testing.T) string {
	t.Helper()

	goModCommand := exec.CommandContext(t.Context(), "go", "env", "GOMOD")
	goModOutput, err := goModCommand.Output()
	require.NoError(t, err)
	moduleRoot := filepath.Dir(string(bytes.TrimSpace(goModOutput)))
	executable := filepath.Join(t.TempDir(), "glyph-tools")
	buildCommand := exec.CommandContext(
		t.Context(),
		"go",
		"build",
		"-o",
		executable,
		"./plugins/extension/tools/cmd/glyph-tools",
	)
	buildCommand.Dir = moduleRoot
	buildOutput, err := buildCommand.CombinedOutput()
	require.NoError(t, err, string(buildOutput))
	return executable
}

// assertRuntimeRunning verifies the runtime process remains available without exposing SDK state.
func assertRuntimeRunning(t *testing.T, runtime *Runtime) {
	t.Helper()

	select {
	case <-runtime.Done():
		assert.Fail(t, "extension runtime exited")
	default:
	}
}

// requireRuntimeStopped verifies lifecycle completion through the consumer-owned signal.
func requireRuntimeStopped(t *testing.T, runtime *Runtime) {
	t.Helper()

	select {
	case <-runtime.Done():
	default:
		require.FailNow(t, "extension runtime did not stop")
	}
}

// discardProgress accepts progress when a test only examines the terminal outcome.
func discardProgress(tool.Progress) error {
	return nil
}
