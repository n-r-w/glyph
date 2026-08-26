package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	ptyInnerEnvironment  = "GLYPH_TUI_PTY_INNER"
	ptyBinaryEnvironment = "GLYPH_TUI_PTY_BINARY"
	ptyTestTimeout       = 30 * time.Second
)

// TestStandardTUIPTY verifies the standard TUI lifecycle against a real pseudo-terminal.
func TestStandardTUIPTY(t *testing.T) {
	t.Parallel()
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
	command := exec.CommandContext(
		ptyContext, "/usr/bin/script", "-q", "/dev/null",
		os.Args[0], "-test.run=^TestStandardTUIPTYInner$",
	)
	command.Env = append(
		os.Environ(),
		ptyInnerEnvironment+"=1",
		ptyBinaryEnvironment+"="+binaryPath,
		"TERM=xterm-256color",
	)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	observer := newOutputObserver(ptyContext)
	command.Stderr = observer
	require.NoError(t, command.Start())
	waiter := newCommandWaiter(command)
	t.Cleanup(func() {
		_ = input.Close()
		_ = command.Process.Kill()
		_ = waiter.Wait()
	})
	copyResult := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(observer, output)
		copyResult <- copyErr
	}()

	observer.WaitNext(t, "Status: Idle")
	observer.WaitNext(t, "Request: |")
	observer.WaitNext(t, "Terminal: 100x40")
	writePTY(t, input, "héllo🙂")
	observer.WaitNext(t, "héllo🙂|")
	writePTY(t, input, "\x1b[13u")

	observer.WaitNext(t, "Running")
	observer.WaitNext(t, "assistant: streaming response")
	writePTY(t, input, string([]byte{3}))

	observer.WaitNext(t, "Idle")
	observer.WaitNext(t, "[tool:status] read (started)")
	observer.WaitNext(t, "[tool:stdout] read content")
	observer.WaitNext(t, "[tool:stderr] read warning")
	// Bubble Tea updates the changed portion of the active model line in place.
	observer.WaitNext(t, "complete ")
	writePTY(t, input, "second request")
	observer.WaitNext(t, "second request|")
	writePTY(t, input, "\x1b[13u")

	observer.WaitNext(t, "Authentication failed")
	observer.WaitNext(t, "[error] Authentication failed safely.")
	writePTY(t, input, string([]byte{18}))

	observer.WaitNext(t, "Idle")
	writePTY(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	runErr := waiter.Wait()
	copyErr := <-copyResult
	require.NoError(t, copyErr)
	require.NoError(t, runErr, observer.String())

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
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Initialization field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Initialization: uiv1.Initialization_builder{
			SelectedUiId: new("glyph-tui"),
			StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
				Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION),
				Text:     new("Glyph session initialized."),
			}.Build()},
			Extensions: []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{
				PluginId: new("glyph-tools"),
				Tools:    []string{"read"},
				Path:     nil,
			}.Build()},
			Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
			Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
				ProviderId: new("openai-codex"),
				ModelId:    new("gpt"),
				Reasoning:  testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
			}.Build()},
			ModelSelection: uiv1.ModelSelection_builder{
				ProviderId:      new("openai-codex"),
				ModelId:         new("gpt"),
				ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
			}.Build(),
		}.Build(),
	}.Build()))
	setTerminalSize(t, terminalFile, 100, 40)

	response, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "héllo🙂", response.GetSubmit().GetText())
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_RUNNING)
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Position: new(int32(0)),
			Text:     new("streaming response"),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
		}.Build(),
		RunId:              nil,
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
	assert.NotNil(t, response.GetStop())
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START),
		ToolCallId:         new("call-1"),
		ToolName:           new("read"),
		RunId:              nil,
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
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
		ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STATUS),
		Text:               new("working"),
		RunId:              nil,
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
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
		ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT),
		Text:               new("content"),
		RunId:              nil,
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
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
		ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR),
		Text:               new("warning"),
		RunId:              nil,
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
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END),
		ToolCallId:         new("call-1"),
		ToolName:           new("read"),
		Text:               new("done"),
		RunId:              nil,
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
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolCallId: new("call-1"),
		ToolName:   new("read"),
		ToolResultContents: []*uiv1.ToolResultContent{
			//nolint:exhaustruct // uiv1.ToolResultContent_builder sets only the active Text field.
			uiv1.ToolResultContent_builder{
				Text: new("result"),
			}.Build(),
		},
		RunId:           nil,
		Text:            nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
		ModelResponse: uiv1.ModelResponse_builder{
			Content: []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				Text: new("complete response"),
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
		RunId:              nil,
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
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED),
		Outcome:            new("completed"),
		RunId:              nil,
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_IDLE)

	response, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "second request", response.GetSubmit().GetText())
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Error field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Error: uiv1.Error_builder{
			Text:                new("Authentication failed safely."),
			RetryAuthentication: new(true),
		}.Build(),
	}.Build()))
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_AUTHENTICATION_FAILED)

	response, err = stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, response.GetRetryAuthentication())
	sendAvailability(t, stream, uiv1.Availability_AVAILABILITY_IDLE)

	response, err = stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, response.GetQuit())
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

// sendAvailability writes one availability transition to the test UI stream.
func sendAvailability(
	t *testing.T,
	stream uiv1.UIService_OpenClient,
	availability uiv1.Availability,
) {
	t.Helper()
	sendLifecycle(t, stream, uiv1.LifecycleEvent_builder{
		Type:         new(uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED),
		Availability: new(availability),
		RunId:        nil,
		Text:         nil,

		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
}

// sendLifecycle writes one lifecycle fixture and requires successful delivery.
func sendLifecycle(t *testing.T, stream uiv1.UIService_OpenClient, lifecycle *uiv1.LifecycleEvent) {
	t.Helper()
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Lifecycle: proto.ValueOrDefault(lifecycle),
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

// setTerminalSize applies the PTY dimensions used by the rendering assertions.
func setTerminalSize(t *testing.T, terminalFile *os.File, width, height int) {
	t.Helper()
	command := exec.CommandContext(
		t.Context(), "/bin/stty", "rows", strconv.Itoa(height), "columns", strconv.Itoa(width),
	)
	command.Stdin = terminalFile
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

// writePTY sends one raw keyboard sequence to the pseudo-terminal.
func writePTY(t *testing.T, writer io.Writer, content string) {
	t.Helper()
	_, err := io.WriteString(writer, content)
	require.NoError(t, err)
}

// outputObserver records PTY bytes and waits for ordered rendering fragments.
type outputObserver struct {
	context      context.Context
	mutex        sync.Mutex
	content      bytes.Buffer
	notification chan struct{}
	cursor       int
}

var _ io.Writer = (*outputObserver)(nil)

// newOutputObserver creates an ordered PTY output cursor bound to the test context.
func newOutputObserver(ctx context.Context) *outputObserver {
	return &outputObserver{
		context:      ctx,
		notification: make(chan struct{}, 1),
		mutex:        sync.Mutex{},
		content:      bytes.Buffer{},
		cursor:       0,
	}
}

// Write records new PTY bytes and wakes blocked rendering assertions.
func (observer *outputObserver) Write(content []byte) (int, error) {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	written, err := observer.content.Write(content)
	select {
	case observer.notification <- struct{}{}:
	default:
	}
	return written, err
}

// WaitNext requires one rendering fragment after the observer cursor.
func (observer *outputObserver) WaitNext(t *testing.T, expected string) {
	t.Helper()
	for {
		observer.mutex.Lock()
		content := observer.content.String()
		position := strings.Index(content[observer.cursor:], expected)
		if position >= 0 {
			observer.cursor += position + len(expected)
			observer.mutex.Unlock()
			return
		}
		observer.mutex.Unlock()

		select {
		case <-observer.notification:
		case <-observer.context.Done():
			t.Fatalf("PTY output did not contain %q after cursor:\n%s", expected, content)
		}
	}
}

// String returns a stable copy of all recorded PTY output.
func (observer *outputObserver) String() string {
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	return observer.content.String()
}

// commandWaiter guarantees one subprocess wait result across cleanup paths.
type commandWaiter struct {
	result error
	done   chan struct{}
}

// newCommandWaiter begins waiting for the isolated TUI subprocess.
func newCommandWaiter(command *exec.Cmd) *commandWaiter {
	waiter := &commandWaiter{
		done:   make(chan struct{}),
		result: nil,
	}
	go func() {
		waiter.result = command.Wait()
		close(waiter.done)
	}()
	return waiter
}

// Wait returns the isolated TUI subprocess result exactly once.
func (waiter *commandWaiter) Wait() error {
	<-waiter.done
	return waiter.result
}

func testUIReasoning(choices ...uiv1.ReasoningChoice) *uiv1.ReasoningCapabilities {
	return uiv1.ReasoningCapabilities_builder{
		Supported:     new(true),
		Choices:       choices,
		DefaultChoice: new(choices[len(choices)-1]),
	}.Build()
}
