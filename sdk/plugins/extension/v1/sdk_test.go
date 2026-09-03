//go:build integration

package extensionv1

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// sdkHelperEnvironment selects the child-process SDK fixture behavior.
	sdkHelperEnvironment = "GLYPH_EXTENSION_SDK_HELPER"
	// sdkStderrDiagnostic is the complete child failure text expected at the caller boundary.
	sdkStderrDiagnostic = "glyph extension SDK child startup diagnostic\n"
)

// TestSDKHelperProcess runs the extension server only in the child process created by the SDK test.
func TestSDKHelperProcess(t *testing.T) {
	t.Parallel()

	// Arrange: select the child-process behavior from its isolated environment.
	service := newContractService(t)

	// Act: serve the selected protocol version or emit the selected startup failure.
	switch os.Getenv(sdkHelperEnvironment) {
	case "serve":
		Serve(service)
	case "version-2":
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: handshakeConfig(),
			TLSProvider:     nil,
			Plugins:         nil,
			VersionedPlugins: map[int]plugin.PluginSet{
				2: {
					pluginName: &grpcExtensionPlugin{
						NetRPCUnsupportedPlugin: plugin.NetRPCUnsupportedPlugin{},
						server:                  newServer(service),
					},
				},
			},
			GRPCServer: plugin.DefaultGRPCServer,
			Logger:     nil,
			Test:       nil,
		})
	case "stderr-failure":
		_, _ = fmt.Fprint(os.Stderr, sdkStderrDiagnostic)
		os.Exit(23)
	}

	// Assert: go-plugin owns child-process termination after Serve returns.
}

// TestConnectAndServe verifies public SDK operations, progress, results, and clean shutdown.
func TestConnectAndServe(t *testing.T) {
	t.Parallel()

	// Arrange: configure this test executable to become an extension child process.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), sdkHelperEnvironment+"=serve")

	// Act: complete the handshake and open the operation stream.
	client, err := Connect(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	assert.Equal(t, ProtocolVersion, client.NegotiatedVersion())

	registerRequest := new(extensionpb.HostRequest)
	registerRequest.SetRegister(new(extensionpb.RegisterRequest))
	registrationOperation, err := connection.Start(t.Context(), "register", registerRequest)
	require.NoError(t, err)
	registration, err := registrationOperation.Wait(t.Context(), nil)
	require.NoError(t, err)
	catalog := registration.GetRegister()

	// Assert: registration preserves the complete public catalog.
	require.Len(t, catalog.GetTools(), 1)
	assert.Equal(t, "contract", catalog.GetTools()[0].GetName())
	assert.Equal(
		t,
		extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_REQUIRE,
		catalog.GetTools()[0].GetConstrainedSampling().GetJsonSchema().GetStrictness(),
	)
	require.Len(t, catalog.GetHandlers(), 1)
	assert.Equal(t, "observer", catalog.GetHandlers()[0].GetId())

	// Act: invoke the registered observer through a Handle operation.
	handleRequest := new(extensionpb.HostRequest)
	//nolint:exhaustruct_v5 // The request builder sets only the active observer payload.
	handleRequest.SetHandle(extensionpb.HandleRequest_builder{
		HandlerId: new("observer"),
		SessionTree: extensionpb.SessionTreeInvocation_builder{
			SessionId: new("session"), TargetEntryId: new("target"),
			PrecedingActiveLeafId: nil, NavigationDestinationId: nil,
			CommittedActiveLeafId: nil, CreatedSummary: nil,
		}.Build(),
	}.Build())
	handleOperation, err := connection.Start(t.Context(), "handle", handleRequest)
	require.NoError(t, err)
	handleCompleted, err := handleOperation.Wait(t.Context(), nil)
	require.NoError(t, err)

	// Assert: preserve the typed observer acknowledgement.
	require.NotNil(t, handleCompleted.GetHandle().GetSessionTree())

	// Act: request an unknown tool through the public operation API.
	rejectedRequest := new(extensionpb.HostRequest)
	rejectedRequest.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("unknown"), ArgumentsJson: []byte(`{}`),
	}.Build())
	rejectedOperation, err := connection.Start(t.Context(), "rejected", rejectedRequest)
	require.NoError(t, err)
	_, rejectedErr := rejectedOperation.Wait(t.Context(), nil)

	// Assert: preserve the exact rejected category and complete text.
	var rejection *RejectionError
	require.ErrorAs(t, rejectedErr, &rejection)
	assert.Equal(t, rejectionCodeInvalidArgument, rejection.Code())
	require.EqualError(t, rejectedErr, assert.AnError.Error())

	// Act: execute accepted work that fails through the public operation API.
	failedRequest := new(extensionpb.HostRequest)
	failedRequest.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("contract"), ArgumentsJson: []byte(`{}`),
	}.Build())
	failedOperation, err := connection.Start(t.Context(), "failed", failedRequest)
	require.NoError(t, err)
	_, failedErr := failedOperation.Wait(t.Context(), nil)

	// Assert: preserve INTERNAL and complete failure text.
	var failure *FailureError
	require.ErrorAs(t, failedErr, &failure)
	assert.Equal(t, failureCodeInternal, failure.Code())
	require.EqualError(t, failedErr, "complete operation failure text")

	// Act: execute another tool operation and collect ordered progress.
	executeRequest := new(extensionpb.HostRequest)
	executeRequest.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("contract"), ArgumentsJson: []byte(`{}`),
	}.Build())
	executeOperation, err := connection.Start(t.Context(), "execute", executeRequest)
	require.NoError(t, err)
	progress := make([]string, 0, 1)
	executeCompleted, err := executeOperation.Wait(t.Context(), func(event *extensionpb.ToolProgress) error {
		progress = append(progress, event.GetContent())
		return nil
	})
	require.NoError(t, err)
	result := executeCompleted.GetTool()

	// Assert: preserve progress and ordered text and image result data.
	assert.Equal(t, []string{"started"}, progress)
	assert.True(t, result.GetIsError())
	require.Len(t, result.GetContents(), 2)
	assert.Equal(t, "done", result.GetContents()[0].GetText())
	assert.Equal(t, "image/png", result.GetContents()[1].GetImage().GetMediaType())
	assert.Equal(t, []byte{0, 1, 2, 3}, result.GetContents()[1].GetImage().GetData())

	// Act: request normal stream closure and stop the process.
	require.NoError(t, connection.Close())
	client.Close()

	// Assert: closing waits for go-plugin process termination.
	assert.True(t, client.Exited())
	select {
	case <-client.Done():
	default:
		assert.Fail(t, "client lifecycle signal remained open after process exit")
	}
}

// newContractService creates generated mocks for the valid public SDK fixture.
func newContractService(t *testing.T) Service {
	t.Helper()
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	handler := NewMockHandleOperation(controller)
	execution := NewMockExecuteOperation(controller)

	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil).AnyTimes()
	registration.EXPECT().Run(gomock.Any()).DoAndReturn(
		func(context.Context) (*extensionpb.RegisterResponse, error) {
			return contractRegistration(), nil
		},
	).AnyTimes()
	registration.EXPECT().Release().AnyTimes()

	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request *extensionpb.HandleRequest) (HandleOperation, error) {
			if request.GetHandlerId() != "observer" || request.GetSessionTree() == nil {
				return nil, Reject("INVALID_ARGUMENT", assert.AnError)
			}
			return handler, nil
		},
	).AnyTimes()
	handler.EXPECT().Run(gomock.Any()).Return(extensionpb.HandleResponse_builder{
		SessionBeforeTreeRequest: nil,
		SessionBeforeTreeResult:  nil,
		SessionTree:              extensionpb.SessionTreeAction_builder{}.Build(),
		Error:                    nil,
	}.Build(), nil).AnyTimes()
	handler.EXPECT().Release().AnyTimes()

	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request *extensionpb.ExecuteRequest) (ExecuteOperation, error) {
			if request.GetToolName() != "contract" {
				return nil, Reject("INVALID_ARGUMENT", assert.AnError)
			}
			return execution, nil
		},
	).AnyTimes()
	var executionCalls atomic.Int64
	execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, reporter *ProgressReporter) (*extensionpb.ToolResult, error) {
			if executionCalls.Add(1) == 1 {
				return nil, Fail(failureCodeInternal, errors.New("complete operation failure text"))
			}
			if err := reporter.Report(ctx, extensionpb.ToolProgress_builder{
				Channel: new(extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS), Content: new("started"),
			}.Build()); err != nil {
				return nil, err
			}
			return contractToolResult(), nil
		},
	).AnyTimes()
	execution.EXPECT().Release().AnyTimes()
	return service
}

// contractRegistration returns the valid public SDK fixture catalog.
func contractRegistration() *extensionpb.RegisterResponse {
	return extensionpb.RegisterResponse_builder{
		Tools: []*extensionpb.ToolDescriptor{extensionpb.ToolDescriptor_builder{
			Name: new("contract"), Description: new("Contract test tool."), InputSchemaJson: []byte(`{}`),
			//nolint:exhaustruct_v5 // The builder sets only the active JSON Schema field.
			ConstrainedSampling: extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_REQUIRE),
				}.Build(),
			}.Build(),
		}.Build()},
		Handlers: []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
			Id: new("observer"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
		}.Build()},
	}.Build()
}

// contractToolResult returns ordered text and image result data.
func contractToolResult() *extensionpb.ToolResult {
	return extensionpb.ToolResult_builder{
		IsError: new(true),
		Contents: []*extensionpb.ToolResultContent{
			//nolint:exhaustruct_v5 // The content builder sets only text.
			extensionpb.ToolResultContent_builder{Text: new("done")}.Build(),
			//nolint:exhaustruct_v5 // The content builder sets only image.
			extensionpb.ToolResultContent_builder{Image: extensionpb.ToolResultImage_builder{
				MediaType: new("image/png"), Data: []byte{0, 1, 2, 3},
			}.Build()}.Build(),
		},
	}.Build()
}

// TestConnectPreservesChildStderr verifies startup failure output crosses the public SDK process boundary.
func TestConnectPreservesChildStderr(t *testing.T) {
	t.Parallel()

	// Arrange: capture the complete startup error from a direct go-plugin failure.
	causeCommand := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	causeCommand.Env = append(os.Environ(), sdkHelperEnvironment+"=stderr-failure")
	causeProcess := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:     handshakeConfig(),
		Plugins:             nil,
		VersionedPlugins:    pluginSets(nil),
		Cmd:                 causeCommand,
		Reattach:            nil,
		RunnerFunc:          nil,
		SecureConfig:        nil,
		TLSConfig:           nil,
		Managed:             false,
		MinPort:             0,
		MaxPort:             0,
		StartTimeout:        startTimeout,
		Stderr:              nil,
		SyncStdout:          nil,
		SyncStderr:          nil,
		AllowedProtocols:    []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:              hclog.NewNullLogger(),
		PluginLogBufferSize: 0,
		AutoMTLS:            false,
		GRPCDialOptions:     nil,
		GRPCBrokerMultiplex: false,
		SkipHostEnv:         false,
		UnixSocketConfig:    nil,
	})
	_, originalCause := causeProcess.Client()
	causeProcess.Kill()
	require.Error(t, originalCause)

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), sdkHelperEnvironment+"=stderr-failure")
	var stderr bytes.Buffer
	command.Stderr = &stderr

	// Act: connect while the child writes its diagnostic and exits unsuccessfully.
	client, err := Connect(t.Context(), command)

	// Assert: preserve every diagnostic byte and the complete wrapped go-plugin cause.
	require.EqualError(t, err, "connect extension process: "+originalCause.Error())
	unwrapped := errors.Unwrap(err)
	require.Error(t, unwrapped)
	require.EqualError(t, unwrapped, originalCause.Error())
	assert.Nil(t, client)
	assert.Equal(t, sdkStderrDiagnostic, stderr.String())
}

// TestConnectRejectsVersionMismatch verifies no compatibility path outside protocol version 1.
func TestConnectRejectsVersionMismatch(t *testing.T) {
	t.Parallel()

	// Arrange: configure a child process that offers only extension protocol version 2.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKHelperProcess$")
	command.Env = append(os.Environ(), sdkHelperEnvironment+"=version-2")

	// Act: attempt the public SDK handshake.
	client, err := Connect(t.Context(), command)

	// Assert: reject the incompatible process without fallback.
	assert.Nil(t, client)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "incompatible api version")
}

// TestConnectRejectsCanceledContext verifies process startup respects prior cancellation.
func TestConnectRejectsCanceledContext(t *testing.T) {
	t.Parallel()

	// Arrange: cancel before any process may start.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act: attempt to connect with the canceled context.
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSDKHelperProcess$")
	client, err := Connect(ctx, command)

	// Assert: return no client and preserve cancellation.
	assert.Nil(t, client)
	require.ErrorIs(t, err, context.Canceled)
}
