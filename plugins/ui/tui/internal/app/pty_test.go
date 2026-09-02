//go:build integration

package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/types/known/timestamppb"

	testsupporttui "github.com/n-r-w/glyph/internal/testsupport/tui"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	ptyInnerEnvironment  = "GLYPH_TUI_PTY_INNER"
	ptyBinaryEnvironment = "GLYPH_TUI_PTY_BINARY"
	ptyTestTimeout       = 30 * time.Second
	ptyJoinTimeout       = 5 * time.Second
)

// TestStandardTUIPTY verifies the standard TUI lifecycle against a real pseudo-terminal.
func TestStandardTUIPTY(t *testing.T) {
	t.Parallel()
	// Arrange the TUI executable, Host stream, and real pseudo-terminal input sequence.
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY acceptance runs on Darwin arm64")
	}

	workspaceRoot, err := filepath.Abs("../../../../..")
	require.NoError(t, err)
	binaryPath := filepath.Join(t.TempDir(), "glyph-tui")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, "./plugins/ui/tui/cmd/glyph-tui")
	build.Dir = workspaceRoot
	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, string(buildOutput))

	ptyContext, cancelPTY := context.WithTimeout(t.Context(), ptyTestTimeout)
	t.Cleanup(cancelPTY)
	// Cleanup owns wrapper cancellation because testing cancels t.Context before cleanup runs.
	wrapperContext, cancelWrapper := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(
		wrapperContext, "/usr/bin/script", "-q", "/dev/null",
		os.Args[0], "-test.run=^TestStandardTUIPTYInner$",
	)
	command.Env = append(
		os.Environ(),
		ptyInnerEnvironment+"=1",
		ptyBinaryEnvironment+"="+binaryPath,
		"TERM=xterm-256color",
	)
	testsupporttui.ConfigureProcessGroup(command)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	observer := testsupporttui.NewOutputObserver(ptyContext)
	command.Stderr = observer

	// Act by starting the PTY process and driving submit, cancel, authentication, and exit input.
	require.NoError(t, command.Start())
	waiter := testsupporttui.NewCommandWaiter(command)
	outputWaiter := testsupporttui.NewOutputWaiter(observer, output)
	testsupporttui.RegisterProcessGroupCleanup(t.Context(), t, testsupporttui.ProcessGroupCleanup{
		Cancel:        cancelWrapper,
		Input:         input,
		Command:       command,
		CommandWaiter: waiter,
		OutputWaiter:  outputWaiter,
		Timeout:       ptyJoinTimeout,
	})

	observer.WaitNext(t, "Status: Idle")
	observer.WaitNext(t, "Request: |")
	observer.WaitNext(t, "Terminal: 100x40")
	testsupporttui.Write(t, input, "héllo🙂")
	observer.WaitNext(t, "héllo🙂|")
	testsupporttui.Write(t, input, "\x1b[13u")

	observer.WaitNext(t, "Running")
	observer.WaitNext(t, "assistant: streaming response")
	testsupporttui.Write(t, input, string([]byte{3}))

	observer.WaitNext(t, "Idle")
	observer.WaitNext(t, "[tool:status] read (started)")
	observer.WaitNext(t, "[tool:stdout] read content")
	observer.WaitNext(t, "[tool:stderr] read warning")
	// Bubble Tea updates the changed portion of the active model line in place.
	observer.WaitNext(t, "complete ")
	testsupporttui.Write(t, input, "second request")
	observer.WaitNext(t, "second request|")
	testsupporttui.Write(t, input, "\x1b[13u")

	observer.WaitNext(t, "Authentication failed")
	observer.WaitNext(t, "[error] Authentication failed safely.")
	testsupporttui.Write(t, input, string([]byte{18}))

	observer.WaitNext(t, "Idle")
	testsupporttui.Write(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	runErr := waiter.Wait(ptyContext)
	copyErr := outputWaiter.Wait(ptyContext)
	require.NoError(t, copyErr)
	require.NoError(t, runErr, observer.String())

	// Assert the process exits cleanly and the rendered transcript contains each retained lifecycle result.
	rendered := observer.String()
	assert.Contains(t, rendered, "user: héllo🙂")
	assert.Contains(t, rendered, "user: second request")
	assert.Contains(t, rendered, "[tool:done] read result")
	assert.NotContains(t, rendered, "[info] completed")
	assert.Contains(t, rendered, "PASS")
}

// TestStandardTUIPTYInner serves the isolated UI process exercised by the outer PTY test.
func TestStandardTUIPTYInner(t *testing.T) {
	t.Parallel()
	// Arrange the subprocess environment, terminal descriptors, SDK stream, and initialization payload.
	if os.Getenv(ptyInnerEnvironment) == "" {
		return
	}

	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	originalSettings := terminalSettings(t, terminalFile)

	client, err := uisdk.Connect(t.Context(), exec.CommandContext(
		t.Context(), os.Getenv(ptyBinaryEnvironment),
	))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	stream, err := client.Service().Open(t.Context())
	require.NoError(t, err)
	initialization := uiv1.Initialization_builder{
		SelectedUiId: new("glyph-tui"),
		StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
			Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION), Text: new("Glyph session initialized."),
		}.Build()},
		Extensions: []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{
			PluginId: new("glyph-tools"), Tools: []string{"read"}, Path: new(""),
		}.Build()},
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
		Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt"),
			Reasoning: testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build()},
		ModelSelection: uiv1.ModelSelection_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
		SessionInfo: uiv1.SessionInfo_builder{
			Id: new("session-1"), Name: nil, WorkingDirectory: new("/project"), StoragePath: nil,
			CreatedTime: timestamppb.New(time.UnixMilli(1)), UpdateTime: timestamppb.New(time.UnixMilli(1)),
		}.Build(),
	}.Build()
	initializeRequest := new(uiv1.HostRequest)
	initializeRequest.SetInitialize(initialization)
	// Act by initializing the UI stream and serving each operation requested by the outer PTY process.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: initializeRequest, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
	}
	testsupporttui.SetTerminalSize(t, terminalFile, 100, 40)

	response, err := stream.Recv()
	require.NoError(t, err)
	firstID := response.GetOperationId()
	// Assert the first request preserves Unicode input before sending its lifecycle.
	assert.Equal(t, "héllo🙂", response.GetRequest().GetSubmit().GetText())
	beginHostOperation(t, stream, firstID)
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_RUNNING)
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Position: new(int64(0)),
			Text:     new("streaming response"),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
		}.Build(),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())

	response, err = stream.Recv()
	require.NoError(t, err)
	cancelID := response.GetOperationId()
	assert.Equal(t, firstID, response.GetRequest().GetCancel().GetTargetOperationId())
	beginHostOperation(t, stream, cancelID)
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START),
		ToolCallId:         new("call-1"),
		ToolName:           new("read"),
		RunId:              new("run"),
		Text:               nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
		ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STATUS),
		Text:               new("working"),
		RunId:              new("run"),
		ToolCallId:         nil,
		ToolName:           nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
		ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT),
		Text:               new("content"),
		RunId:              new("run"),
		ToolCallId:         nil,
		ToolName:           nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
		ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR),
		Text:               new("warning"),
		RunId:              new("run"),
		ToolCallId:         nil,
		ToolName:           nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END),
		ToolCallId:         new("call-1"),
		ToolName:           new("read"),
		Text:               new("done"),
		RunId:              new("run"),
		ProgressChannel:    nil,
		IsError:            new(false),
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolCallId: new("call-1"),
		ToolName:   new("read"),
		ToolResultContents: []*uiv1.ToolResultContent{
			//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Text field.
			uiv1.ToolResultContent_builder{
				Text: new("result"),
			}.Build(),
		},
		RunId:           new("run"),
		Text:            nil,
		ProgressChannel: nil,
		IsError:         new(false),
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	sendLifecycle(t, stream, firstID, uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
		ModelResponse: uiv1.ModelResponse_builder{
			Content: []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				Text: new("complete response"), ToolCall: nil,
			}.Build()},
			Text:          nil,
			Outcome:       nil,
			ErrorMessage:  nil,
			Provider:      nil,
			Model:         nil,
			ResponseId:    nil,
			Usage:         nil,
			Diagnostics:   nil,
			ResponseModel: nil,
		}.Build(),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	completeSubmitOperation(t, stream, firstID)
	completeCancellationOperation(t, stream, cancelID)
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_IDLE)

	response, err = stream.Recv()
	require.NoError(t, err)
	secondID := response.GetOperationId()
	assert.Equal(t, "second request", response.GetRequest().GetSubmit().GetText())
	beginHostOperation(t, stream, secondID)
	sendConnectionError(t, stream, "AUTHENTICATION_FAILED", "Authentication failed safely.")
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_AUTHENTICATION_FAILED)
	failHostOperation(t, stream, secondID, "CREDENTIAL_UNAVAILABLE", "Authentication failed safely.")

	response, err = stream.Recv()
	require.NoError(t, err)
	retryID := response.GetOperationId()
	assert.NotNil(t, response.GetRequest().GetRetryAuthentication())
	beginHostOperation(t, stream, retryID)
	completeAuthenticationOperation(t, stream, retryID)
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_IDLE)

	response, err = stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, response.GetClose())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(""), Request: nil, Event: nil, ConnectionEvent: nil,
		Close: new(operationv1.CloseConnection),
	}.Build()))
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(
		t,
		normalizeTerminalSettings(originalSettings),
		normalizeTerminalSettings(terminalSettings(t, terminalFile)),
	)

	client.Close()
	select {
	case <-client.Done():
	case <-t.Context().Done():
		t.Fatal("TUI plugin process did not stop")
	}
	assert.True(t, client.Exited())
}

// sendAvailability writes one connection availability transition.
func sendAvailability(t *testing.T, stream uiv1.UIService_OpenClient, availability uiv1.Availability) {
	t.Helper()
	event := new(uiv1.HostConnectionEvent)
	event.SetAvailabilityChanged(uiv1.AvailabilityChanged_builder{Availability: new(availability)}.Build())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(""), Request: nil, Event: nil, ConnectionEvent: event, Close: nil,
	}.Build()))
}

// sendConnectionError writes one classified connection error.
func sendConnectionError(t *testing.T, stream uiv1.UIService_OpenClient, code, text string) {
	t.Helper()
	event := new(uiv1.HostConnectionEvent)
	event.SetError(uiv1.Error_builder{Code: new(code), Text: new(text)}.Build())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(""), Request: nil, Event: nil, ConnectionEvent: event, Close: nil,
	}.Build()))
}

// beginHostOperation delivers accepted and running lifecycle events.
func beginHostOperation(t *testing.T, stream uiv1.UIService_OpenClient, operationID string) {
	t.Helper()
	accepted := new(uiv1.HostEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	sendHostEvent(t, stream, operationID, accepted)
	running := new(uiv1.HostEvent)
	running.SetRunning(new(operationv1.Running))
	sendHostEvent(t, stream, operationID, running)
}

// sendLifecycle writes one correlated agent progress event.
func sendLifecycle(t *testing.T, stream uiv1.UIService_OpenClient, operationID string, lifecycle *uiv1.AgentEvent) {
	t.Helper()
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(lifecycle)
	event := new(uiv1.HostEvent)
	event.SetProgress(progress)
	sendHostEvent(t, stream, operationID, event)
}

// completeSubmitOperation completes one submit operation.
func completeSubmitOperation(t *testing.T, stream uiv1.UIService_OpenClient, operationID string) {
	t.Helper()
	completed := new(uiv1.HostCompleted)
	completed.SetSubmit(new(uiv1.SubmitCompleted))
	completeHostOperation(t, stream, operationID, completed)
}

// completeAuthenticationOperation completes one authentication retry.
func completeAuthenticationOperation(t *testing.T, stream uiv1.UIService_OpenClient, operationID string) {
	t.Helper()
	completed := new(uiv1.HostCompleted)
	completed.SetAuthentication(new(uiv1.AuthenticationCompleted))
	completeHostOperation(t, stream, operationID, completed)
}

// completeCancellationOperation completes one cancellation after its target completed.
func completeCancellationOperation(t *testing.T, stream uiv1.UIService_OpenClient, operationID string) {
	t.Helper()
	completed := new(uiv1.HostCompleted)
	completed.SetCancel(operationv1.CancelCompleted_builder{
		TargetState: new(operationv1.TerminalState_TERMINAL_STATE_COMPLETED),
	}.Build())
	completeHostOperation(t, stream, operationID, completed)
}

// completeHostOperation delivers one completed lifecycle payload.
func completeHostOperation(
	t *testing.T,
	stream uiv1.UIService_OpenClient,
	operationID string,
	completed *uiv1.HostCompleted,
) {
	t.Helper()
	event := new(uiv1.HostEvent)
	event.SetCompleted(completed)
	sendHostEvent(t, stream, operationID, event)
}

// failHostOperation delivers one classified failed lifecycle event.
func failHostOperation(t *testing.T, stream uiv1.UIService_OpenClient, operationID, code, message string) {
	t.Helper()
	event := new(uiv1.HostEvent)
	event.SetFailed(operationv1.Failed_builder{Code: new(code), Message: new(message)}.Build())
	sendHostEvent(t, stream, operationID, event)
}

// sendHostEvent writes one correlated Host operation event.
func sendHostEvent(t *testing.T, stream uiv1.UIService_OpenClient, operationID string, event *uiv1.HostEvent) {
	t.Helper()
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(operationID), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
	}.Build()))
}

// terminalSettings reads the current terminal mode for restoration comparison.
func terminalSettings(t *testing.T, terminalFile *os.File) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "/bin/stty", "-g")
	command.Stdin = terminalFile
	output, err := command.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(output))
}

// normalizeTerminalSettings removes platform display differences from terminal modes.
func normalizeTerminalSettings(settings string) string {
	parts := strings.Split(settings, ":")
	for index, part := range parts {
		if !strings.HasPrefix(part, "lflag=") {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(part, "lflag="), 16, 64)
		if err != nil {
			return settings
		}
		parts[index] = "lflag=" + strconv.FormatUint(value&^uint64(syscall.PENDIN), 16)
	}
	return strings.Join(parts, ":")
}

func testUIReasoning(choices ...uiv1.ReasoningChoice) *uiv1.ReasoningCapabilities {
	return uiv1.ReasoningCapabilities_builder{
		Supported:     new(true),
		Choices:       choices,
		DefaultChoice: new(choices[len(choices)-1]),
	}.Build()
}
