package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	runtimeHelperEnvironment = "GLYPH_EXTENSION_RUNTIME_HELPER"
	runtimeCountEnvironment  = "GLYPH_EXTENSION_RUNTIME_COUNT"
	validSchemaJSON          = `{"type":"object","properties":{"path":{"type":"string","description":"File path."}},"required":["path"],"additionalProperties":false}`
	processOperationTimeout  = 10 * time.Second
)

// protocolService emits selected contract behaviors from a real helper process.
type protocolService struct {
	extensionpb.UnimplementedExtensionServiceServer
	mode      string
	countPath string
}

// executionOutcome carries a concurrent execution result back to the test goroutine.
type executionOutcome struct {
	result tool.Result
	err    error
}

// fifoOpenOutcome reports when the production read operation has opened its blocking FIFO.
type fifoOpenOutcome struct {
	file *os.File
	err  error
}

// TestFactoryRuntimeSurvivesStartupContextCancellation verifies explicit Host shutdown owns process lifetime.
func TestFactoryRuntimeSurvivesStartupContextCancellation(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join(t.TempDir(), "glyph-test-extension")
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=default exec %q -test.run=^TestRuntimeHelperProcess$\n",
		runtimeHelperEnvironment,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o700))
	startupContext, cancelStartup := context.WithCancel(t.Context())
	runtime, err := NewFactory().Start(startupContext, toolservice.Candidate{ID: "test", Path: scriptPath})
	require.NoError(t, err)
	cancelStartup()

	select {
	case <-runtime.Done():
		require.Fail(t, "extension process stopped before explicit Host shutdown")
	case <-time.After(200 * time.Millisecond):
	}
	descriptors, err := runtime.ListTools(t.Context())
	require.NoError(t, err)
	assert.Len(t, descriptors, 1)

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

	mode := os.Getenv(runtimeHelperEnvironment)
	if mode == "" {
		return
	}
	extensionsdk.Serve(&protocolService{
		UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
		mode:                                mode,
		countPath:                           os.Getenv(runtimeCountEnvironment),
	})
}

// TestRuntimeWithRealGlyphTools verifies the production process handshake, read descriptor, execution, validation, and shutdown.
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
	tools, err := runtime.ListTools(t.Context())
	require.NoError(t, err)

	// Assert: expose the complete standard read, edit, and bash catalog.
	require.Len(t, tools, 3)
	assert.Equal(t, "read", tools[0].Name)
	assert.NotEmpty(t, tools[0].Description)
	assert.NotEmpty(t, tools[0].InputSchemaJSON)

	// Act: read a relative project file through the real finite execution stream.
	result, err := runtime.Execute(
		t.Context(),
		"read",
		[]byte(`{"path":"notes.txt"}`),
		discardProgress,
	)

	// Assert: preserve complete text in exactly one terminal successful result.
	require.NoError(t, err)
	assert.Equal(t, tool.Result{Content: "first\nsecond\n", IsError: false}, result)

	// Act: replace one unique fragment through the production edit tool.
	editResult, err := runtime.Execute(
		t.Context(),
		"edit",
		[]byte(`{"path":"notes.txt","oldText":"first","newText":"updated"}`),
		discardProgress,
	)
	require.NoError(t, err)
	assert.False(t, editResult.IsError)
	editedContent, err := os.ReadFile(filepath.Join(projectDirectory, "notes.txt"))
	require.NoError(t, err)
	assert.Equal(t, "updated\nsecond\n", string(editedContent))

	// Act: stream both output channels and return a nonzero terminal bash result.
	bashProgress := make([]tool.ProgressChannel, 0, 3)
	bashResult, err := runtime.Execute(
		t.Context(),
		"bash",
		[]byte(`{"command":"printf out; printf err >&2; exit 7"}`),
		func(progress tool.Progress) error {
			bashProgress = append(bashProgress, progress.Channel)
			return nil
		},
	)
	require.NoError(t, err)
	assert.True(t, bashResult.IsError)
	assert.JSONEq(t, `{"stdout":"out","stderr":"err","exitCode":7}`, bashResult.Content)
	assert.Contains(t, bashProgress, tool.ProgressChannelStatus)
	assert.Contains(t, bashProgress, tool.ProgressChannelStdout)
	assert.Contains(t, bashProgress, tool.ProgressChannelStderr)

	// Act: submit arguments outside the cached descriptor schema.
	invalidResult, err := runtime.Execute(t.Context(), "read", []byte(`{}`), discardProgress)

	// Assert: reject them as a terminal tool error without making the process unavailable.
	require.NoError(t, err)
	assert.True(t, invalidResult.IsError)
	assert.NotEmpty(t, invalidResult.Content)
	assertRuntimeRunning(t, runtime)

	// Arrange: a FIFO keeps the production read operation active until the Host cancels its stream.
	fifoPath := filepath.Join(projectDirectory, "blocking-input")
	require.NoError(t, syscall.Mkfifo(fifoPath, 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	executionChannel := make(chan executionOutcome, 1)
	go func() {
		executionResult, executionErr := runtime.Execute(
			ctx,
			"read",
			[]byte(`{"path":"blocking-input"}`),
			discardProgress,
		)
		executionChannel <- executionOutcome{result: executionResult, err: executionErr}
	}()

	// Act: opening the writer proves glyph-tools reached the blocking read before cancellation.
	fifoChannel := make(chan fifoOpenOutcome, 1)
	go func() {
		fifo, fifoErr := os.OpenFile(fifoPath, os.O_WRONLY, 0o600)
		fifoChannel <- fifoOpenOutcome{file: fifo, err: fifoErr}
	}()
	var fifo *os.File
	select {
	case openOutcome := <-fifoChannel:
		require.NoError(t, openOutcome.err)
		fifo = openOutcome.file
	case <-time.After(processOperationTimeout):
		require.FailNow(t, "glyph-tools did not start the blocking read")
	}
	cancel()

	// Assert: active cancellation crosses the real process boundary without stopping the runtime.
	select {
	case execution := <-executionChannel:
		assert.Equal(t, tool.Result{Content: "", IsError: false}, execution.result)
		require.ErrorIs(t, execution.err, context.Canceled)
	case <-time.After(processOperationTimeout):
		require.FailNow(t, "glyph-tools did not return after cancellation")
	}
	require.NoError(t, fifo.Close())
	assertRuntimeRunning(t, runtime)

	// Act: stop the extension process through the Host runtime adapter.
	runtime.Close()

	// Assert: shutdown waits until the process has exited.
	requireRuntimeStopped(t, runtime)
}

func TestRuntimeValidatesCachedSchemaBeforeExtensionRPC(t *testing.T) {
	t.Parallel()

	countPath := filepath.Join(t.TempDir(), "executions")
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeHelperProcess$")
	command.Env = append(os.Environ(), runtimeHelperEnvironment+"=default", runtimeCountEnvironment+"="+countPath)
	runtime, err := Start(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	_, err = runtime.ListTools(t.Context())
	require.NoError(t, err)

	unknown, err := runtime.Execute(t.Context(), "missing", []byte(`{}`), discardProgress)
	require.NoError(t, err)
	require.True(t, unknown.IsError)
	invalid, err := runtime.Execute(t.Context(), "read", []byte(`{}`), discardProgress)
	require.NoError(t, err)
	require.True(t, invalid.IsError)
	_, statErr := os.Stat(countPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	valid, err := runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress)
	require.NoError(t, err)
	require.False(t, valid.IsError)
	count, err := os.ReadFile(countPath)
	require.NoError(t, err)
	require.Equal(t, "1\n", string(count))
}

// TestRuntimePropagatesActiveCancellation verifies cancellation of an in-flight streamed execution.
func TestRuntimePropagatesActiveCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: start a helper process that reports readiness and then waits for stream cancellation.
	runtime := startHelperRuntime(t, "wait")
	_, err := runtime.ListTools(t.Context())
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
		outcome <- executionOutcome{result: result, err: executeErr}
	}()
	<-started
	cancel()
	execution := <-outcome

	// Assert: cancellation remains identifiable and does not become a protocol violation.
	require.ErrorIs(t, execution.err, context.Canceled)
	assert.Equal(t, tool.Result{Content: "", IsError: false}, execution.result)
	assertRuntimeRunning(t, runtime)
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
			_, err := runtime.ListTools(t.Context())
			require.NoError(t, err)

			// Act: consume the malformed finite stream.
			result, err := runtime.Execute(
				t.Context(),
				"read",
				[]byte(`{"path":"notes.txt"}`),
				discardProgress,
			)

			// Assert: fail the call, stop the violating process, and return no terminal payload.
			assert.Equal(t, tool.Result{Content: "", IsError: false}, result)
			require.Error(t, err)
			require.ErrorIs(t, err, toolservice.ErrExtensionUnavailable)
			require.ErrorContains(t, err, "extension protocol violation")
			requireRuntimeStopped(t, runtime)
		})
	}
}

// TestRuntimeRejectsInvalidCatalogs verifies complete-catalog validation before tools enter Host state.
func TestRuntimeRejectsInvalidCatalogs(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"empty-name",
		"empty-description",
		"invalid-schema-json",
		"schema-outside-profile",
		"duplicate-name",
	}
	for _, mode := range testCases {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			// Arrange: start a real helper process with one invalid complete catalog.
			runtime := startHelperRuntime(t, mode)

			// Act: request catalog validation and caching.
			tools, err := runtime.ListTools(t.Context())

			// Assert: reject the complete catalog and stop only its owning process.
			assert.Nil(t, tools)
			require.Error(t, err)
			require.ErrorContains(t, err, "validate extension catalog")
			requireRuntimeStopped(t, runtime)
		})
	}
}

// TestRuntimeProgressDeliveryFailurePreservesProcess keeps a healthy extension available.
func TestRuntimeProgressDeliveryFailurePreservesProcess(t *testing.T) {
	t.Parallel()

	runtime := startHelperRuntime(t, "progress")
	_, err := runtime.ListTools(t.Context())
	require.NoError(t, err)
	deliveryErr := errors.New("event consumer failed")

	_, err = runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), func(tool.Progress) error {
		return deliveryErr
	})
	require.ErrorIs(t, err, deliveryErr)
	require.NotErrorIs(t, err, toolservice.ErrExtensionUnavailable)
	assertRuntimeRunning(t, runtime)

	result, err := runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress)
	require.NoError(t, err)
	assert.Equal(t, tool.Result{Content: "done", IsError: false}, result)
}

// TestRuntimeClassifiesTransportFailure marks the closed runtime unavailable.
func TestRuntimeClassifiesTransportFailure(t *testing.T) {
	t.Parallel()

	runtime := startHelperRuntime(t, "transport-error")
	_, err := runtime.ListTools(t.Context())
	require.NoError(t, err)

	_, err = runtime.Execute(t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress)

	require.ErrorIs(t, err, toolservice.ErrExtensionUnavailable)
	requireRuntimeStopped(t, runtime)
}

// TestRuntimeForwardsProgress verifies ordered progress delivery before the terminal result.
func TestRuntimeForwardsProgress(t *testing.T) {
	t.Parallel()

	// Arrange: start a helper process that emits one status fragment and one result.
	runtime := startHelperRuntime(t, "progress")
	_, err := runtime.ListTools(t.Context())
	require.NoError(t, err)
	progress := make([]tool.Progress, 0, 1)

	// Act: consume the valid streamed execution.
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
	assert.Equal(t, []tool.Progress{{Channel: tool.ProgressChannelStatus, Content: "working"}}, progress)
	assert.Equal(t, tool.Result{Content: "done", IsError: false}, result)
}

// ListTools returns a valid or deliberately invalid catalog selected by the helper mode.
func (s *protocolService) ListTools(
	_ context.Context,
	_ *extensionpb.ListToolsRequest,
) (*extensionpb.ListToolsResponse, error) {
	descriptor := &extensionpb.ToolDescriptor{
		Name:                "read",
		Description:         "Read a project file.",
		InputSchemaJson:     []byte(validSchemaJSON),
		ConstrainedSampling: nil,
	}
	response := &extensionpb.ListToolsResponse{Tools: []*extensionpb.ToolDescriptor{descriptor}}

	switch s.mode {
	case "empty-name":
		descriptor.Name = ""
	case "empty-description":
		descriptor.Description = ""
	case "invalid-schema-json":
		descriptor.InputSchemaJson = []byte(`{"type":`)
	case "schema-outside-profile":
		descriptor.InputSchemaJson = []byte(`{"type":"object","properties":{},"required":[],"additionalProperties":false,"title":"not allowed"}`)
	case "duplicate-name":
		response.Tools = append(response.Tools, descriptor)
	}
	return response, nil
}

// Execute emits one behavior selected by the helper mode.
func (s *protocolService) Execute(
	_ *extensionpb.ExecuteRequest,
	stream extensionpb.ExtensionService_ExecuteServer,
) error {
	if s.countPath != "" {
		file, err := os.OpenFile(s.countPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err = file.WriteString("1\n"); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
	}
	progress := &extensionpb.ExecuteResponse{
		Content: &extensionpb.ExecuteResponse_Progress{
			Progress: &extensionpb.ToolProgress{
				Channel: extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS,
				Content: "working",
			},
		},
	}
	result := &extensionpb.ExecuteResponse{
		Content: &extensionpb.ExecuteResponse_Result{
			Result: &extensionpb.ToolResult{Content: "done", IsError: false},
		},
	}

	switch s.mode {
	case "missing-result":
		return nil
	case "duplicate-result":
		if err := stream.Send(result); err != nil {
			return err
		}
		return stream.Send(result)
	case "event-after-result":
		if err := stream.Send(result); err != nil {
			return err
		}
		return stream.Send(progress)
	case "empty-event":
		return stream.Send(&extensionpb.ExecuteResponse{Content: nil})
	case "wait":
		if err := stream.Send(progress); err != nil {
			return err
		}
		<-stream.Context().Done()
		return status.FromContextError(stream.Context().Err()).Err()
	case "transport-error":
		return status.Error(codes.Unavailable, "transport failed")
	default:
		if err := stream.Send(progress); err != nil {
			return err
		}
		return stream.Send(result)
	}
}

// startHelperRuntime starts this test binary as a real extension process.
func startHelperRuntime(t *testing.T, mode string) *Runtime {
	t.Helper()

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeHelperProcess$")
	command.Env = append(os.Environ(), runtimeHelperEnvironment+"="+mode)
	runtime, err := Start(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	return runtime
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
