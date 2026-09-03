//go:build integration

package extensionv1

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// hostDisconnectTestTimeout bounds every stage of the transport-loss integration test.
	hostDisconnectTestTimeout = 5 * time.Second
	// hostDisconnectBlockedCheckTimeout bounds the observation that Open remains blocked at Release.
	hostDisconnectBlockedCheckTimeout = 100 * time.Millisecond
)

// TestHostClientDisconnectWaitsForActiveExecuteRelease verifies transport-loss cleanup at the public gRPC boundary.
func TestHostClientDisconnectWaitsForActiveExecuteRelease(t *testing.T) {
	t.Parallel()

	// Arrange an SDK service with controlled Execute Run and Release stages.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	runStarted := make(chan struct{})
	runStopped := make(chan struct{})
	releaseStarted := make(chan struct{})
	releaseGate := make(chan struct{})
	releaseFinished := make(chan struct{})
	openAfterRelease := make(chan bool, 1)
	serveResult := make(chan error, 1)
	var releaseGateOnce sync.Once

	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
	execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
		ctx context.Context,
		_ *ProgressReporter,
	) (*extensionpb.ToolResult, error) {
		close(runStarted)
		<-ctx.Done()
		close(runStopped)
		return nil, context.Cause(ctx)
	})
	execution.EXPECT().Release().Do(func() {
		close(releaseStarted)
		<-releaseGate
		close(releaseFinished)
	})

	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcServer := grpc.NewServer(grpc.StreamInterceptor(func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		handlerErr := handler(srv, stream)
		select {
		case <-releaseFinished:
			openAfterRelease <- true
		default:
			openAfterRelease <- false
		}
		return handlerErr
	}))
	extensionpb.RegisterExtensionServiceServer(grpcServer, newServer(service))
	go func() { serveResult <- grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		cleanupContext, cancelCleanup := context.WithTimeout(
			context.WithoutCancel(t.Context()),
			hostDisconnectTestTimeout,
		)
		defer cancelCleanup()
		serverStopFinished := make(chan struct{})
		go func() {
			grpcServer.Stop()
			close(serverStopFinished)
		}()
		awaitHostDisconnectSignal(t, cleanupContext, serverStopFinished, "gRPC server stop")
		if serveErr := awaitHostDisconnectSignal(
			t,
			cleanupContext,
			serveResult,
			"gRPC server shutdown",
		); serveErr != nil {
			assert.ErrorIs(t, serveErr, grpc.ErrServerStopped)
		}
	})

	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	var connectionCloseOnce sync.Once
	closeConnection := func() error {
		var closeErr error
		connectionCloseOnce.Do(func() { closeErr = connection.Close() })
		return closeErr
	}
	t.Cleanup(func() { assert.NoError(t, closeConnection()) })
	t.Cleanup(func() { releaseGateOnce.Do(func() { close(releaseGate) }) })
	openContext, cancelOpen := context.WithTimeout(t.Context(), hostDisconnectTestTimeout)
	t.Cleanup(cancelOpen)
	stream, err := extensionpb.NewExtensionServiceClient(connection).Open(openContext)
	require.NoError(t, err)

	require.NoError(t, stream.Send(integrationRegisterRequest("register")))
	for range 3 {
		response := receiveHostDisconnectResponse(t, stream, "Register lifecycle")
		assert.Equal(t, "register", response.GetOperationId())
	}
	require.NoError(t, stream.Send(integrationExecuteRequest("execute")))
	for range 2 {
		response := receiveHostDisconnectResponse(t, stream, "Execute startup lifecycle")
		assert.Equal(t, "execute", response.GetOperationId())
	}
	awaitHostDisconnectSignal(t, openContext, runStarted, "Execute Run start")

	// Act by disconnecting the actual Host gRPC client while Execute is active.
	require.NoError(t, closeConnection())
	awaitHostDisconnectSignal(t, openContext, runStopped, "Execute Run cancellation")
	awaitHostDisconnectSignal(t, openContext, releaseStarted, "Execute Release start")

	// Assert Open remains blocked while Release waits at its gate.
	requireHostDisconnectSignalBlocked(t, openContext, openAfterRelease, "Open RPC completion")
	releaseGateOnce.Do(func() { close(releaseGate) })

	// Assert the server-side Open call returns only after Release completes.
	awaitHostDisconnectSignal(t, openContext, releaseFinished, "Execute Release completion")
	assert.True(t, awaitHostDisconnectSignal(t, openContext, openAfterRelease, "Open RPC completion"),
		"extension server completed before Execute Release")
}

// receiveHostDisconnectResponse receives one public response within the Open RPC deadline.
func receiveHostDisconnectResponse(
	t *testing.T,
	stream extensionpb.ExtensionService_OpenClient,
	stage string,
) *extensionpb.OpenResponse {
	t.Helper()
	response, err := stream.Recv()
	require.NoError(t, err, "receive %s", stage)
	return response
}

// awaitHostDisconnectSignal waits for one controlled test stage within ctx's deadline.
func awaitHostDisconnectSignal[T any](t *testing.T, ctx context.Context, signal <-chan T, stage string) T {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v", stage, context.Cause(ctx))
		var zero T
		return zero
	}
}

// requireHostDisconnectSignalBlocked checks that a stage does not complete while Release is gated.
func requireHostDisconnectSignalBlocked[T any](
	t *testing.T,
	parent context.Context,
	signal <-chan T,
	stage string,
) {
	t.Helper()
	checkContext, cancelCheck := context.WithTimeout(parent, hostDisconnectBlockedCheckTimeout)
	defer cancelCheck()
	select {
	case <-signal:
		t.Fatalf("%s finished while Execute Release remained blocked", stage)
	case <-checkContext.Done():
		require.ErrorIs(t, context.Cause(checkContext), context.DeadlineExceeded,
			"bounded check for %s ended unexpectedly", stage)
	}
}

// integrationRegisterRequest creates a public Register operation request.
func integrationRegisterRequest(operationID string) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetRegister(new(extensionpb.RegisterRequest))
	return extensionpb.OpenRequest_builder{
		OperationId: new(operationID),
		Request:     request,
		Close:       nil,
	}.Build()
}

// integrationExecuteRequest creates a public Execute operation request.
func integrationExecuteRequest(operationID string) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName:      new("contract"),
		ArgumentsJson: []byte(`{}`),
	}.Build())
	return extensionpb.OpenRequest_builder{
		OperationId: new(operationID),
		Request:     request,
		Close:       nil,
	}.Build()
}
