//go:build integration

package runtime

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// blockedHostUIRequestSize exceeds the bounded HTTP/2 receive window.
	blockedHostUIRequestSize = 2 * 1024 * 1024
	// hostUICleanupWatchdog only fails a test when production cleanup does not finish.
	hostUICleanupWatchdog = 3 * time.Second
)

// blockedHostUIPeer keeps the real RPC open without reading Host requests.
type blockedHostUIPeer struct {
	uiv1.UnimplementedUIServiceServer
	// started reports that the real server handler owns the RPC.
	started chan<- struct{}
	// stopped reports that actual RPC cancellation released the handler.
	stopped chan<- struct{}
}

// startupCleanupPeer returns initialization failure and controls its close handshake.
type startupCleanupPeer struct {
	uiv1.UnimplementedUIServiceServer
	// completeHandshake selects whether the peer reads the Host close request and returns.
	completeHandshake bool
	// failureSent reports that initialization failure entered the real transport.
	failureSent chan<- struct{}
	// closeReceived reports receipt of the graceful CloseConnection request.
	closeReceived chan<- struct{}
	// stopped reports handler termination after graceful completion or RPC cancellation.
	stopped chan<- struct{}
}

// observedHostUIClientStream records Send and CloseSend overlap around a real gRPC stream.
type observedHostUIClientStream struct {
	uiv1.UIService_OpenClient
	// sendStarted reports entry into the first real Send call.
	sendStarted chan struct{}
	// sendStartedOnce closes sendStarted once.
	sendStartedOnce sync.Once
	// activeSends counts real Send calls that have not returned.
	activeSends atomic.Int64
	// closeOverlappedSend reports whether CloseSend ran during Send.
	closeOverlappedSend atomic.Bool
	// cancel releases the invalid overlap so the test can report an assertion failure.
	cancel context.CancelFunc
}

// Open keeps the RPC alive until the Host cancels its actual stream context.
func (peer *blockedHostUIPeer) Open(stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse]) error {
	if err := stream.Send(operationRequest("active", "work")); err != nil {
		return err
	}
	close(peer.started)
	<-stream.Context().Done()
	close(peer.stopped)
	return stream.Context().Err()
}

// Open reports failed initialization and either completes or blocks the close handshake.
func (peer *startupCleanupPeer) Open(stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse]) error {
	request, err := stream.Recv()
	if err != nil {
		return err
	}
	if request.GetRequest().GetInitialize() == nil {
		return errors.New("expected initialization request")
	}
	for _, event := range startupFailureEvents() {
		if err = stream.Send(event); err != nil {
			return err
		}
	}
	close(peer.failureSent)
	if peer.completeHandshake {
		request, err = stream.Recv()
		if err != nil {
			return err
		}
		if request.GetClose() == nil {
			return errors.New("expected CloseConnection request")
		}
		close(peer.closeReceived)
		close(peer.stopped)
		return nil
	}
	<-stream.Context().Done()
	close(peer.stopped)
	return stream.Context().Err()
}

// Send observes one real gRPC send without replacing transport behavior.
func (stream *observedHostUIClientStream) Send(request *uiv1.OpenRequest) error {
	stream.activeSends.Add(1)
	stream.sendStartedOnce.Do(func() { close(stream.sendStarted) })
	defer stream.activeSends.Add(-1)
	return stream.UIService_OpenClient.Send(request)
}

// CloseSend records invalid overlap before delegating to the real gRPC stream.
func (stream *observedHostUIClientStream) CloseSend() error {
	if stream.activeSends.Load() != 0 {
		stream.closeOverlappedSend.Store(true)
		stream.cancel()
		return errors.New("CloseSend overlapped active Send")
	}
	return stream.UIService_OpenClient.CloseSend()
}

// TestFatalBlockedHostUISendCancelsRPCBeforeCloseSend verifies transport-aware fatal cleanup.
func TestFatalBlockedHostUISendCancelsRPCBeforeCloseSend(t *testing.T) {
	t.Parallel()

	// Arrange a real peer that never reads and an observed real client stream.
	peerStarted := make(chan struct{})
	peerStopped := make(chan struct{})
	peer := &blockedHostUIPeer{
		UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
		started:                      peerStarted,
		stopped:                      peerStopped,
	}
	stream, cancelStream, stopServer := openHostUITestStream(t, peer)
	defer stopServer()
	observed := &observedHostUIClientStream{
		UIService_OpenClient: stream,
		sendStarted:          make(chan struct{}),
		sendStartedOnce:      sync.Once{},
		activeSends:          atomic.Int64{},
		closeOverlappedSend:  atomic.Bool{},
		cancel:               cancelStream,
	}
	service := New()
	service.stream = observed
	service.cancel = cancelStream
	activated := make(chan struct{})
	operationStarted := make(chan struct{})
	prepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](gomock.NewController(t))
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
		ctx context.Context,
		_ operation.Reporter[controllerui.Frame],
	) operation.Outcome[controllerui.Frame] {
		close(operationStarted)
		<-ctx.Done()
		return operation.Canceled[controllerui.Frame]()
	})
	prepared.EXPECT().Release()
	result := make(chan error, 1)
	go func() {
		result <- runTestOperations(t, service, t.Context(), func() { close(activated) }, func(
			context.Context,
			controllerui.Command,
		) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
			return prepared, nil
		})
	}()
	awaitHostUISignal(t, activated, "controller activation")
	awaitHostUISignal(t, operationStarted, "active operation start")

	// Act by blocking the real Send and then filling the production writer queue.
	largeCause := errors.New(strings.Repeat("x", blockedHostUIRequestSize))
	require.NoError(t, service.ReportError("BLOCKED", largeCause))
	awaitHostUISignal(t, observed.sendStarted, "real Host UI Send start")
	awaitHostUISignal(t, peerStarted, "real Host UI server handler start")
	var admissionErr error
	for admissionErr == nil {
		admissionErr = service.ReportError("FILL", errors.New("fill Host UI writer"))
	}
	require.ErrorIs(t, admissionErr, operation.ErrQueueFull)
	err := awaitHostUIResult(t, result, "fatal Host UI cleanup")

	// Assert RPC cancellation released Send and Recv before non-overlapping CloseSend and handler cleanup.
	require.Error(t, err)
	require.ErrorIs(t, err, operation.ErrQueueFull)
	assert.Zero(t, observed.activeSends.Load())
	assert.False(t, observed.closeOverlappedSend.Load())
	awaitHostUISignal(t, peerStopped, "real Host UI server handler cleanup")
}

// TestUnsuccessfulInitializationCleanupUsesGraceThenCancelsRPC verifies both startup cleanup outcomes.
func TestUnsuccessfulInitializationCleanupUsesGraceThenCancelsRPC(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name              string
		completeHandshake bool
		expectTimeout     bool
	}{
		{name: "responsive peer", completeHandshake: true, expectTimeout: false},
		{name: "blocked receive", completeHandshake: false, expectTimeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a real peer that fails initialization and controls close completion.
			failureSent := make(chan struct{})
			closeReceived := make(chan struct{})
			peerStopped := make(chan struct{})
			peer := &startupCleanupPeer{
				UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
				completeHandshake:            test.completeHandshake,
				failureSent:                  failureSent,
				closeReceived:                closeReceived,
				stopped:                      peerStopped,
			}
			stream, cancelStream, stopServer := openHostUITestStream(t, peer)
			defer stopServer()
			service := New()
			service.stream = stream
			service.cancel = cancelStream
			result := make(chan error, 1)

			// Act through failed initialization and its production graceful cleanup.
			go func() { result <- service.Initialize(t.Context(), testInitialization()) }()
			awaitHostUISignal(t, failureSent, "initialization failure delivery")
			err := awaitHostUIResult(t, result, "unsuccessful initialization cleanup")

			// Assert the source remains and timeout appears only when the peer blocks the handshake.
			require.Error(t, err)
			require.ErrorContains(t, err, "peer rejected startup")
			if test.expectTimeout {
				require.ErrorContains(t, err, "UI unsuccessful initialization cleanup timed out after 1s")
			} else {
				require.NotErrorIs(t, err, context.DeadlineExceeded)
				require.NotContains(t, err.Error(), "timed out")
				awaitHostUISignal(t, closeReceived, "graceful CloseConnection receipt")
			}
			awaitHostUISignal(t, peerStopped, "startup peer handler cleanup")
		})
	}
}

// openHostUITestStream starts one real local UI gRPC stream.
func openHostUITestStream(
	t *testing.T,
	peer uiv1.UIServiceServer,
) (uiv1.UIService_OpenClient, context.CancelFunc, func()) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.InitialWindowSize(1024), grpc.InitialConnWindowSize(1024))
	uiv1.RegisterUIServiceServer(server, peer)
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(1024),
		grpc.WithInitialConnWindowSize(1024),
	)
	require.NoError(t, err)
	streamContext, cancelStream := context.WithCancel(t.Context())
	stream, err := uiv1.NewUIServiceClient(connection).Open(streamContext)
	require.NoError(t, err)
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancelStream()
			require.NoError(t, connection.Close())
			server.Stop()
			serveErr := <-serveResult
			if serveErr != nil {
				require.ErrorIs(t, serveErr, grpc.ErrServerStopped)
			}
			_ = listener.Close()
		})
	}
	t.Cleanup(stop)
	return stream, cancelStream, stop
}

// startupFailureEvents builds the valid lifecycle that ends initialization unsuccessfully.
func startupFailureEvents() []*uiv1.OpenResponse {
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	failed := new(uiv1.UIEvent)
	failed.SetFailed(operationv1.Failed_builder{
		Code: new("INIT_FAIL"), Message: new("peer rejected startup"),
	}.Build())
	return []*uiv1.OpenResponse{
		uiv1.OpenResponse_builder{
			OperationId: new(initializationOperationID), Request: nil, Event: accepted, Close: nil,
		}.Build(),
		uiv1.OpenResponse_builder{
			OperationId: new(initializationOperationID), Request: nil, Event: running, Close: nil,
		}.Build(),
		uiv1.OpenResponse_builder{
			OperationId: new(initializationOperationID), Request: nil, Event: failed, Close: nil,
		}.Build(),
	}
}

// awaitHostUISignal waits for a synchronized event and uses time only to fail the test.
func awaitHostUISignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(hostUICleanupWatchdog):
		t.Fatalf("timed out waiting for %s", name)
	}
}

// awaitHostUIResult waits for a synchronized result and uses time only to fail the test.
func awaitHostUIResult(t *testing.T, result <-chan error, name string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(hostUICleanupWatchdog):
		t.Fatalf("timed out waiting for %s", name)
		return nil
	}
}
