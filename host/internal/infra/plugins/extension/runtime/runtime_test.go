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
	"syscall"
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
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// runtimeHelperEnvironment selects the child-process fixture behavior.
	runtimeHelperEnvironment = "GLYPH_EXTENSION_RUNTIME_HELPER"
	// runtimeCountEnvironment provides the path used to count remote executions.
	runtimeCountEnvironment = "GLYPH_EXTENSION_RUNTIME_COUNT"
	// processOperationTimeout bounds real child-process coordination.
	processOperationTimeout = 10 * time.Second
)

// protocolService prepares selected operations in a real helper process.
type protocolService struct {
	// mode selects the fixture behavior.
	mode string
	// countPath records admitted tool executions when configured.
	countPath string
}

// protocolRegisterOperation returns one mode-specific registration.
type protocolRegisterOperation struct {
	// service owns the selected fixture mode.
	service *protocolService
}

// protocolHandleOperation returns one observer action.
type protocolHandleOperation struct{}

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

// fifoOpenOutcome reports when the production read operation has opened its blocking FIFO.
type fifoOpenOutcome struct {
	// file is the FIFO writer opened after the extension begins reading.
	file *os.File
	// err contains the FIFO open failure.
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
	runtime, err := NewFactory().Start(startupContext, extensionservice.Candidate{
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
	fixture := &protocolService{mode: mode, countPath: os.Getenv(runtimeCountEnvironment)}
	extensionsdk.Serve(newProtocolMockService(t, fixture))

	// Assert: go-plugin owns child-process lifetime after the selected server starts.
}

// newProtocolMockService creates generated SDK mocks for one valid child-process fixture.
func newProtocolMockService(t *testing.T, fixture *protocolService) extensionsdk.Service {
	t.Helper()
	controller := gomock.NewController(t)
	service := extensionsdk.NewMockService(controller)
	registration := extensionsdk.NewMockRegisterOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil).AnyTimes()
	registration.EXPECT().Run(gomock.Any()).DoAndReturn(
		func(context.Context) (*extensionpb.RegisterResponse, error) {
			return (&protocolRegisterOperation{service: fixture}).Run(t.Context())
		},
	).AnyTimes()
	registration.EXPECT().Release().AnyTimes()
	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *extensionpb.HandleRequest) (extensionsdk.HandleOperation, error) {
			prepared, err := fixture.PrepareHandle(ctx, request)
			if err != nil {
				return nil, err
			}
			handler := extensionsdk.NewMockHandleOperation(controller)
			handler.EXPECT().Run(gomock.Any()).DoAndReturn(prepared.Run)
			handler.EXPECT().Release()
			return handler, nil
		},
	).AnyTimes()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request *extensionpb.ExecuteRequest) (extensionsdk.ExecuteOperation, error) {
			prepared, err := fixture.PrepareExecute(ctx, request)
			if err != nil {
				return nil, err
			}
			execution := extensionsdk.NewMockExecuteOperation(controller)
			execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(prepared.Run)
			execution.EXPECT().Release()
			return execution, nil
		},
	).AnyTimes()
	return service
}

// isAdversarialMode reports whether one helper mode bypasses the SDK to violate the public protocol.
func isAdversarialMode(mode string) bool {
	switch mode {
	case "missing-result", "duplicate-result", "event-after-result", "empty-event",
		"empty-result", "mismatched-handler":
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
		executionChannel <- executionOutcome{
			result: executionResult,
			err:    executionErr,
		}
	}()

	// Act: opening the writer proves glyph-tools reached the blocking read before cancellation.
	fifoChannel := make(chan fifoOpenOutcome, 1)
	go func() {
		fifo, fifoErr := os.OpenFile(fifoPath, os.O_WRONLY, 0o600)
		fifoChannel <- fifoOpenOutcome{
			file: fifo,
			err:  fifoErr,
		}
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
		assert.Equal(t, tool.Result{
			Contents: nil,
			IsError:  false,
		}, execution.result)
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

// TestCompileToolSchemaAcceptsJSONCompatibleArguments verifies nested and optional schema values.
func TestCompileToolSchemaAcceptsJSONCompatibleArguments(t *testing.T) {
	t.Parallel()

	// Arrange: define an object schema with nested and optional values.
	schema, err := compileToolSchema([]byte(`{
		"type":"object",
		"properties":{
			"text":{"type":"string"},
			"number":{"type":"number"},
			"enabled":{"type":"boolean"},
			"nullable":{"type":["string","null"]},
			"items":{"type":"array","items":{}},
			"nested":{"type":"object"},
			"optional":{"type":"string"}
		},
		"required":["text","number","enabled","nullable","items","nested"],
		"additionalProperties":false
	}`))
	require.NoError(t, err)

	// Act: validate one complete argument object.
	validErr := validateArguments(schema, []byte(`{
		"text":"value","number":12.5,"enabled":true,"nullable":null,
		"items":[1,"two",false,null,{"child":3}],"nested":{"child":[true]}
	}`))

	// Assert: accept complete input and reject missing required values.
	require.NoError(t, validErr)
	require.Error(t, validateArguments(schema, []byte(`{"text":"value"}`)))
}

// TestCompileToolSchemaRejectsNonObjectRoot keeps provider tool arguments object-shaped.
func TestCompileToolSchemaRejectsNonObjectRoot(t *testing.T) {
	t.Parallel()

	// Arrange: define a schema with an array root.
	schemaJSON := []byte(`{"type":"array","items":{"type":"string"}}`)

	// Act: compile the invalid tool schema.
	_, err := compileToolSchema(schemaJSON)

	// Assert: reject the non-object root with its schema rule.
	require.ErrorContains(t, err, "root type must be object")
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

// TestRuntimeValidatesCachedSchemaBeforeExtensionOperation verifies validation prevents invalid stream work.
func TestRuntimeValidatesCachedSchemaBeforeExtensionOperation(t *testing.T) {
	t.Parallel()

	// Arrange: start a counting extension and complete registration.
	countPath := filepath.Join(t.TempDir(), "executions")
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeHelperProcess$")
	command.Env = append(os.Environ(), runtimeHelperEnvironment+"=default", runtimeCountEnvironment+"="+countPath)
	runtime, err := Start(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	_, err = runtime.Register(t.Context())
	require.NoError(t, err)

	// Act: execute unknown, schema-invalid, and valid tool requests.
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

	// Assert: only the valid request reaches the extension operation.
	require.NoError(t, err)
	require.Equal(t, "1\n", string(count))
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
			require.ErrorIs(t, err, extensionservice.ErrExtensionUnavailable)
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
			request := extensionservice.HandlerRequest{
				SessionBeforeTreeRequest: mo.None[extensionservice.SessionBeforeTreeRequestInvocation](),
				SessionBeforeTreeResult:  mo.None[extensionservice.SessionBeforeTreeResultInvocation](),
				SessionTree: mo.Some(extensionservice.SessionTreeInvocation{
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
			require.ErrorIs(t, err, extensionservice.ErrExtensionUnavailable)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
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
		"duplicate-name",
	}
	for _, mode := range testCases {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			// Arrange: start a real helper process with one invalid complete catalog.
			runtime := startHelperRuntime(t, mode)

			// Act: request catalog validation and caching.
			registration, err := runtime.Register(t.Context())

			// Assert: reject the complete catalog and stop only its owning process.
			assert.Empty(t, registration)
			require.Error(t, err)
			require.ErrorContains(t, err, "validate extension registration")
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
	require.NotErrorIs(t, err, extensionservice.ErrExtensionUnavailable)
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
	require.ErrorIs(t, err, extensionservice.ErrExtensionUnavailable)
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
	require.Equal(t, []extensionservice.HandlerDescriptor{{
		ID: "observer", Kind: extensionservice.HandlerKindSessionTree,
	}}, registration.Handlers)
	invocation := extensionservice.SessionTreeInvocation{
		SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
		NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
	}
	request := extensionservice.HandlerRequest{
		SessionBeforeTreeRequest: mo.None[extensionservice.SessionBeforeTreeRequestInvocation](),
		SessionBeforeTreeResult:  mo.None[extensionservice.SessionBeforeTreeResultInvocation](),
		SessionTree:              mo.Some(invocation),
	}

	// Act: invoke the registered handler operation.
	response, err := runtime.Handle(t.Context(), "observer", request)

	// Assert: preserve the typed observer acknowledgement.
	require.NoError(t, err)
	assert.Equal(t, extensionservice.HandlerResponse{
		SessionBeforeTreeRequest: mo.None[extensionservice.SessionBeforeTreeRequestAction](),
		SessionBeforeTreeResult:  mo.None[extensionservice.SessionBeforeTreeResultAction](),
		SessionTree:              mo.Some(extensionservice.SessionTreeAction{}),
	}, response)
}

// PrepareRegister admits one mode-specific registration operation.
func (s *protocolService) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &protocolRegisterOperation{service: s}, nil
}

// PrepareHandle validates and admits the fixture observer invocation.
func (s *protocolService) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	if s.mode != "handler" || request.GetHandlerId() != "observer" || request.GetSessionTree() == nil {
		return nil, extensionsdk.Reject("INVALID_ARGUMENT", errors.New("unexpected handler request"))
	}
	if request.GetSessionTree().GetSessionId() != "session" || request.GetSessionTree().GetTargetEntryId() != "target" {
		return nil, extensionsdk.Reject("INVALID_ARGUMENT", errors.New("unexpected observer payload"))
	}
	return &protocolHandleOperation{}, nil
}

// PrepareExecute records and admits one fixture tool execution.
func (s *protocolService) PrepareExecute(
	context.Context,
	*extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
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
	case "handler":
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
	context.Context,
) (*extensionpb.HandleResponse, error) {
	//nolint:exhaustruct_v5 // The response builder sets only the observer action.
	return extensionpb.HandleResponse_builder{
		SessionTree: extensionpb.SessionTreeAction_builder{}.Build(),
	}.Build(), nil
}

// Release has no fixture handler reservation to free.
func (operation *protocolHandleOperation) Release() {}

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
	case "wait":
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

// Release has no fixture execution reservation to free.
func (operation *protocolExecuteOperation) Release() {}

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
