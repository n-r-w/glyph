//go:build integration

package uiv1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// newInitializedMockService configures successful initialization and one service run.
func newInitializedMockService(
	t *testing.T,
	run func(context.Context, *Host) error,
) *MockService {
	t.Helper()
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	initializationOperation := NewMockInitializeOperation(controller)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(run)
	cleanupCompleted := make(chan struct{})
	service.EXPECT().Close().DoAndReturn(func() error {
		close(cleanupCompleted)
		return nil
	})
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			select {
			case <-cleanupCompleted:
				return true
			default:
				return false
			}
		}, time.Second, time.Millisecond)
	})
	return service
}

// newClosingMockService configures one operation followed by local connection closure.
func newClosingMockService(t *testing.T) *MockService {
	t.Helper()
	return newInitializedMockService(t, func(ctx context.Context, host *Host) error {
		request := new(uiv1.UIRequest)
		request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("closing request")}.Build())
		if _, err := host.Start(ctx, "closing-submit", request); err != nil {
			return err
		}
		return host.Close(ctx)
	})
}

// TestCloseDrainsValidEventsToEOF verifies SDK closure coordination.
func TestCloseDrainsValidEventsToEOF(t *testing.T) {
	t.Parallel()

	// Arrange one initialized service that starts work before close.
	client := TestClient(t, newClosingMockService(t))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendIntegrationInitialization(t, stream)
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	started, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "closing-submit", started.GetOperationId())
	closeResponse, err := stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, closeResponse.GetClose())
	closeRequest := new(uiv1.OpenRequest)
	closeRequest.SetClose(new(operationv1.CloseConnection))
	require.NoError(t, stream.Send(closeRequest))

	// Act by delivering valid tracked lifecycle events after CloseConnection.
	accepted := new(uiv1.HostEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.HostEvent)
	running.SetRunning(new(operationv1.Running))
	completedPayload := new(uiv1.HostCompleted)
	completedPayload.SetSubmit(new(uiv1.SubmitCompleted))
	completed := new(uiv1.HostEvent)
	completed.SetCompleted(completedPayload)
	for _, event := range []*uiv1.HostEvent{accepted, running, completed} {
		require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
			OperationId: new("closing-submit"), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
		}.Build()))
	}
	require.NoError(t, stream.CloseSend())

	// Assert response EOF follows complete terminal draining.
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}

// TestCloseFailsNewRequest verifies close-state protocol enforcement without lifecycle rejection.
func TestCloseFailsNewRequest(t *testing.T) {
	t.Parallel()

	// Arrange one locally closing SDK service.
	client := TestClient(t, newClosingMockService(t))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendIntegrationInitialization(t, stream)
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	_, err = stream.Recv()
	require.NoError(t, err)
	_, err = stream.Recv()
	require.NoError(t, err)
	lateRequest := new(uiv1.HostRequest)
	lateRequest.SetCancel(operationv1.CancelOperation_builder{TargetOperationId: new("closing-submit")}.Build())

	// Act by sending a new request after local close started.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("late"), Request: lateRequest, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()

	// Assert FailedPrecondition and no lifecycle rejection.
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "connection is closing")
}

// TestBlockedInitializationSupportsSeparateCancellation verifies startup operation cancellation.
func TestBlockedInitializationSupportsSeparateCancellation(t *testing.T) {
	t.Parallel()

	// Arrange one blocked SDK initialization.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	initializationOperation := NewMockInitializeOperation(controller)
	started := make(chan struct{})
	released := make(chan struct{})
	cleanupCompleted := make(chan struct{})
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).DoAndReturn(func(
		ctx context.Context,
	) (*uiv1.Initialized, error) {
		close(started)
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	initializationOperation.EXPECT().Release().Do(func() { close(released) })
	service.EXPECT().Close().DoAndReturn(func() error {
		close(cleanupCompleted)
		return nil
	})
	client := TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendIntegrationInitialization(t, stream)
	for range 2 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	<-started
	cancelRequest := new(uiv1.HostRequest)
	cancelRequest.SetCancel(operationv1.CancelOperation_builder{
		TargetOperationId: new("initialize"),
	}.Build())

	// Act by starting a separate cancellation operation.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("cancel-initialize"), Request: cancelRequest,
		Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	seenTargetCanceled := false
	seenCancellationCompleted := false
	for !seenTargetCanceled || !seenCancellationCompleted {
		response, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		if response.GetOperationId() == "initialize" && response.GetEvent().GetCanceled() != nil {
			seenTargetCanceled = true
		}
		if response.GetOperationId() == "cancel-initialize" &&
			response.GetEvent().GetCompleted().GetCancel() != nil {
			seenCancellationCompleted = true
			assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED,
				response.GetEvent().GetCompleted().GetCancel().GetTargetState())
		}
	}

	// Assert Release finished and the stream remains open until the caller closes it.
	select {
	case <-released:
	default:
		t.Fatal("initialization cancellation completed before Release")
	}
	require.NoError(t, stream.CloseSend())
	require.Eventually(t, func() bool {
		select {
		case <-cleanupCompleted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

// TestActiveInitializationCleanupWaitsForRelease verifies public close paths join UI-owned startup work.
func TestActiveInitializationCleanupWaitsForRelease(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		trigger func(context.CancelFunc, uiv1.UIService_OpenClient) error
	}{
		{
			name: "peer CloseConnection",
			trigger: func(_ context.CancelFunc, stream uiv1.UIService_OpenClient) error {
				closeRequest := new(uiv1.OpenRequest)
				closeRequest.SetClose(new(operationv1.CloseConnection))
				if sendErr := stream.Send(closeRequest); sendErr != nil {
					return sendErr
				}
				return stream.CloseSend()
			},
		},
		{
			name: "stream context loss",
			trigger: func(cancel context.CancelFunc, _ uiv1.UIService_OpenClient) error {
				cancel()
				return nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange active initialization with separately observable Run, Release, and cleanup stages.
			controller := gomock.NewController(t)
			service := NewMockService(controller)
			prepared := NewMockInitializeOperation(controller)
			runStarted := make(chan struct{})
			runStopped := make(chan struct{})
			releaseStarted := make(chan struct{})
			releaseGate := make(chan struct{})
			releaseFinished := make(chan struct{})
			cleanupFinished := make(chan struct{})
			service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(prepared, nil)
			prepared.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*uiv1.Initialized, error) {
				close(runStarted)
				<-ctx.Done()
				close(runStopped)
				return nil, context.Cause(ctx)
			})
			prepared.EXPECT().Release().Do(func() {
				close(releaseStarted)
				<-releaseGate
				close(releaseFinished)
			})
			service.EXPECT().Close().DoAndReturn(func() error {
				close(cleanupFinished)
				return nil
			})
			client := TestClient(t, service)
			streamContext, cancel := context.WithCancel(t.Context())
			stream, err := client.Open(streamContext)
			require.NoError(t, err)
			sendIntegrationInitialization(t, stream)
			for range 2 {
				_, err = stream.Recv()
				require.NoError(t, err)
			}
			<-runStarted

			// Act through the selected public connection-close path.
			require.NoError(t, test.trigger(cancel, stream))
			<-runStopped
			<-releaseStarted

			// Assert connection cleanup cannot finish before Release returns.
			select {
			case <-cleanupFinished:
				t.Fatal("connection cleanup finished before initialization Release")
			default:
			}
			close(releaseGate)
			<-releaseFinished
			<-cleanupFinished
		})
	}
}

// newBlockedConnectionMockService configures a service that does not consume connection events.
func newBlockedConnectionMockService(t *testing.T) *MockService {
	t.Helper()
	return newInitializedMockService(t, func(ctx context.Context, _ *Host) error {
		<-ctx.Done()
		return context.Cause(ctx)
	})
}

// TestEnvelopeOperationIdentifiersFailStream verifies non-operation identifier invariants.
func TestEnvelopeOperationIdentifiersFailStream(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		message func() *uiv1.OpenRequest
	}{
		{name: "close", message: func() *uiv1.OpenRequest {
			message := new(uiv1.OpenRequest)
			message.SetOperationId("invalid")
			message.SetClose(new(operationv1.CloseConnection))
			return message
		}},
		{name: "connection event", message: func() *uiv1.OpenRequest {
			event := new(uiv1.HostConnectionEvent)
			event.SetInformation(uiv1.Information_builder{Text: new("information")}.Build())
			return uiv1.OpenRequest_builder{
				OperationId: new("invalid"), Request: nil, Event: nil, ConnectionEvent: event, Close: nil,
			}.Build()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one initialized SDK connection.
			client := TestClient(t, newBlockedConnectionMockService(t))
			stream, err := client.Open(t.Context())
			require.NoError(t, err)
			sendIntegrationInitialization(t, stream)
			for range 3 {
				_, err = stream.Recv()
				require.NoError(t, err)
			}

			// Act by sending a non-operation envelope with an operation identifier.
			require.NoError(t, stream.Send(test.message()))
			require.NoError(t, stream.CloseSend())
			_, err = stream.Recv()

			// Assert the protocol fault closes the stream.
			require.Error(t, err)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			assert.ErrorContains(t, err, "operation identifier must be empty")
		})
	}
}

// TestConnectionEventQueueOverflowClosesResourceExhausted verifies bounded queue failure semantics.
func TestConnectionEventQueueOverflowClosesResourceExhausted(t *testing.T) {
	t.Parallel()

	// Arrange one initialized service that does not consume connection events.
	client := TestClient(t, newBlockedConnectionMockService(t))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendIntegrationInitialization(t, stream)
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}

	// Act by exceeding the exact bounded event queue.
	for index := range connectionEventQueueCapacity + 1 {
		event := new(uiv1.HostConnectionEvent)
		event.SetInformation(uiv1.Information_builder{Text: new(fmt.Sprintf("event %d", index))}.Build())
		require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
			OperationId: new(""), Request: nil, Event: nil, ConnectionEvent: event, Close: nil,
		}.Build()))
	}
	_, err = stream.Recv()

	// Assert ResourceExhausted and the complete queue source cause.
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	assert.ErrorContains(t, err, operation.ErrQueueFull.Error())
}

// newConnectionReceiveMockService configures one connection-event delivery attempt.
func newConnectionReceiveMockService(t *testing.T) *MockService {
	t.Helper()
	return newInitializedMockService(t, func(ctx context.Context, host *Host) error {
		_, err := host.Receive(ctx)
		return err
	})
}

// TestMalformedConnectionEventsFailBeforeDelivery verifies required connection-event fields.
func TestMalformedConnectionEventsFailBeforeDelivery(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		event *uiv1.HostConnectionEvent
	}{
		{name: "information text", event: func() *uiv1.HostConnectionEvent {
			event := new(uiv1.HostConnectionEvent)
			event.SetInformation(new(uiv1.Information))
			return event
		}()},
		{name: "error category", event: func() *uiv1.HostConnectionEvent {
			event := new(uiv1.HostConnectionEvent)
			event.SetError(uiv1.Error_builder{Code: new(""), Text: new("failure")}.Build())
			return event
		}()},
		{name: "error text", event: func() *uiv1.HostConnectionEvent {
			event := new(uiv1.HostConnectionEvent)
			event.SetError(uiv1.Error_builder{Code: new("INTERNAL"), Text: new("")}.Build())
			return event
		}()},
		{name: "availability", event: func() *uiv1.HostConnectionEvent {
			event := new(uiv1.HostConnectionEvent)
			event.SetAvailabilityChanged(uiv1.AvailabilityChanged_builder{
				Availability: new(uiv1.Availability_AVAILABILITY_UNSPECIFIED),
			}.Build())
			return event
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange client and stream for the real SDK stream to verify required connection-event fields.
			client := TestClient(t, newConnectionReceiveMockService(t))
			stream, err := client.Open(t.Context())
			require.NoError(t, err)
			sendIntegrationInitialization(t, stream)
			for range 3 {
				_, err = stream.Recv()
				require.NoError(t, err)
			}
			// Act by invoking the real SDK stream to exercise required connection-event fields.
			require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
				OperationId: new(""), Request: nil, Event: nil, ConnectionEvent: test.event, Close: nil,
			}.Build()))

			_, err = stream.Recv()

			// Assert required connection-event fields.
			require.Error(t, err)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			assert.ErrorContains(t, err, test.name)
		})
	}
}

// sendIntegrationInitialization sends one minimal initialization operation.
func sendIntegrationInitialization(t *testing.T, stream uiv1.UIService_OpenClient) {
	t.Helper()
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
}

// newMismatchMockService configures one submit operation for correlation checks.
func newMismatchMockService(t *testing.T) *MockService {
	t.Helper()
	return newInitializedMockService(t, func(ctx context.Context, host *Host) error {
		request := new(uiv1.UIRequest)
		request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("request")}.Build())
		started, err := host.Start(ctx, "submit", request)
		if err != nil {
			return err
		}
		_, err = started.Wait(ctx, nil)
		return err
	})
}

// TestMalformedNestedProgressFailsBeforeCallback verifies SDK-owned nested validation.
func TestMalformedNestedProgressFailsBeforeCallback(t *testing.T) {
	t.Parallel()

	// Arrange an initialized SDK service with one tracked submit operation.
	client := TestClient(t, newMismatchMockService(t))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendIntegrationInitialization(t, stream)
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	request, err := stream.Recv()
	require.NoError(t, err)
	accepted := new(uiv1.HostEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.HostEvent)
	running.SetRunning(new(operationv1.Running))
	for _, event := range []*uiv1.HostEvent{accepted, running} {
		require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
			OperationId: new(request.GetOperationId()), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
		}.Build()))
	}
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: nil, Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil, ModelResponse: nil,
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build())
	malformed := new(uiv1.HostEvent)
	malformed.SetProgress(progress)

	// Act by sending the correlated outer kind without its required nested run ID.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(request.GetOperationId()), Request: nil, Event: malformed, ConnectionEvent: nil, Close: nil,
	}.Build()))
	_, err = stream.Recv()

	// Assert the stream fails before callback or completion delivery.
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "run ID is required")
}

// TestMismatchedCompletedPayloadFailsStream verifies initiating endpoint correlation.
func TestMismatchedCompletedPayloadFailsStream(t *testing.T) {
	t.Parallel()

	// Arrange an initialized SDK service that starts one submit operation.
	client := TestClient(t, newMismatchMockService(t))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	started, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, started.GetRequest().GetSubmit())
	accepted := new(uiv1.HostEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.HostEvent)
	running.SetRunning(new(operationv1.Running))
	wrongCompleted := new(uiv1.HostCompleted)
	wrongCompleted.SetSessionList(new(uiv1.SessionList))
	completed := new(uiv1.HostEvent)
	completed.SetCompleted(wrongCompleted)

	// Act by delivering a wrong completed variant for the tracked submit request.
	for _, event := range []*uiv1.HostEvent{accepted, running, completed} {
		require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
			OperationId: new("submit"), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
		}.Build()))
	}
	_, err = stream.Recv()

	// Assert protocol failure status and complete correlation text.
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "submit")
}

// TestInitializationTerminalPrecedesServiceRun verifies SDK startup lifecycle and application ordering.
func TestInitializationTerminalPrecedesServiceRun(t *testing.T) {
	t.Parallel()

	// Arrange one accepted initialization and an observable Service.Run start.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	prepared := NewMockInitializeOperation(controller)
	runStarted := make(chan struct{})
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(prepared, nil)
	prepared.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	prepared.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ *Host) error {
		close(runStarted)
		<-ctx.Done()
		return context.Cause(ctx)
	})
	cleanupCompleted := make(chan struct{})
	service.EXPECT().Close().DoAndReturn(func() error {
		close(cleanupCompleted)
		return nil
	})
	client := TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))

	// Act by receiving the complete initialization lifecycle.
	accepted, err := stream.Recv()
	require.NoError(t, err)
	running, err := stream.Recv()
	require.NoError(t, err)
	completed, err := stream.Recv()
	require.NoError(t, err)

	// Assert the terminal result is delivered before Service.Run starts.
	assert.NotNil(t, accepted.GetEvent().GetAccepted())
	assert.NotNil(t, running.GetEvent().GetRunning())
	assert.NotNil(t, completed.GetEvent().GetCompleted().GetInitialized())
	require.Eventually(t, func() bool {
		select {
		case <-runStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.NoError(t, stream.CloseSend())
	require.Eventually(t, func() bool {
		select {
		case <-cleanupCompleted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

// TestServiceCleanupPreservesRunAndCloseCauses verifies connection cleanup error joining.
func TestServiceCleanupPreservesRunAndCloseCauses(t *testing.T) {
	t.Parallel()

	// Arrange initialized service startup with distinct run and cleanup failures.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	prepared := NewMockInitializeOperation(controller)
	runCause := errors.New("start UI application failed")
	closeCause := errors.New("close prepared terminal failed")
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(prepared, nil)
	prepared.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	prepared.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).Return(runCause)
	service.EXPECT().Close().Return(closeCause)
	client := TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}

	// Act by receiving server failure after Service.Run and Service.Close.
	_, err = stream.Recv()

	// Assert both complete causes remain visible.
	require.Error(t, err)
	require.ErrorContains(t, err, runCause.Error())
	require.ErrorContains(t, err, closeCause.Error())
}

const uiSDKIntegrationMode = "GLYPH_UI_SDK_INTEGRATION_MODE"

// TestSDKIntegrationHelperProcess serves the UI SDK from the test subprocess.
func TestSDKIntegrationHelperProcess(t *testing.T) {
	t.Parallel()
	// Arrange the generated service mock selected by the subprocess protocol mode.
	// Act by serving the current or unsupported handshake from the subprocess.
	// Assert the subprocess exposes only the protocol selected by its mode.

	controller := gomock.NewController(t)
	service := NewMockService(controller)
	switch os.Getenv(uiSDKIntegrationMode) {
	case "current":
		Serve(service)
	case "unsupported":
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: unsupportedHandshakeConfig(),
			TLSProvider:     nil, Plugins: nil,
			VersionedPlugins: map[int]plugin.PluginSet{
				2: pluginSets(newServer(service))[ProtocolVersion],
			},
			GRPCServer: plugin.DefaultGRPCServer, Logger: nil, Test: nil,
		})
	}
}

// unsupportedHandshakeConfig creates the rejected unsupported handshake.
func unsupportedHandshakeConfig() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion: 2, MagicCookieKey: magicCookieKey, MagicCookieValue: magicCookieValue,
	}
}

// TestConnectNegotiatesRequiredProtocol verifies required handshake and cookie compatibility.
func TestConnectNegotiatesRequiredProtocol(t *testing.T) {
	t.Parallel()

	// Arrange one current UI plugin process.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKIntegrationHelperProcess$")
	command.Env = append(os.Environ(), uiSDKIntegrationMode+"=current")

	// Act through the public process SDK.
	client, err := Connect(t.Context(), command)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	// Assert negotiation selects only the required protocol value.
	assert.Equal(t, ProtocolVersion, client.NegotiatedVersion())
}

// TestConnectRejectsUnsupportedProtocol verifies no compatibility negotiation path exists.
func TestConnectRejectsUnsupportedProtocol(t *testing.T) {
	t.Parallel()

	// Arrange one process with an unsupported protocol value.
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSDKIntegrationHelperProcess$")
	command.Env = append(os.Environ(), uiSDKIntegrationMode+"=unsupported")

	// Act through the public process SDK.
	client, err := Connect(t.Context(), command)

	// Assert the unsupported protocol cannot connect.
	require.Error(t, err)
	assert.Nil(t, client)
}
