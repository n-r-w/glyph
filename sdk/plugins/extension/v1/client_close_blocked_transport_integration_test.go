//go:build integration

package extensionv1

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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/n-r-w/glyph/internal/testsupport/gatedconn"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// blockedClientPayloadSize exceeds the HTTP/2 stream window while the Host transport does not read.
	blockedClientPayloadSize = 2 * 1024 * 1024
	// blockedClientTCPBufferSize limits data buffered before the client Send reaches transport backpressure.
	blockedClientTCPBufferSize = 4 * 1024
	// blockedClientWatchdogTimeout only fails a test when synchronized cleanup does not complete.
	blockedClientWatchdogTimeout = 5 * time.Second
	// blockedClientRequestCount fills gRPC transport buffers before the observed raw Send starts.
	blockedClientRequestCount = 65
	// blockedClientOperationID identifies the second request whose raw Send lifecycle the test observes.
	blockedClientOperationID = "blocked-send"
)

// clientCloseHost is a connected Host peer that either receives requests or waits without receiving.
type clientCloseHost struct {
	extensionpb.UnimplementedExtensionServiceServer

	// receive selects whether Open processes the graceful close protocol.
	receive bool
	// connected signals that the real gRPC Open handler is active.
	connected chan struct{}
	// closeReceived signals delivery of CloseConnection to a responsive Host.
	closeReceived chan struct{}
	// release allows the blocked Host handler to return during test cleanup.
	release chan struct{}
	// releaseOnce limits cleanup release to one caller.
	releaseOnce sync.Once
}

// clientSendObserver records the raw gRPC SendMsg lifecycle for the large request.
type clientSendObserver struct {
	// ClientStream is the real generated gRPC client stream.
	grpc.ClientStream
	// targetStarted signals entry into the observed raw SendMsg call.
	targetStarted chan struct{}
	// targetResult reports when the observed raw SendMsg call returns.
	targetResult chan error
	// targetOnce limits the start signal to the observed request.
	targetOnce sync.Once
	// targetPending records whether the observed raw SendMsg call has returned.
	targetPending atomic.Bool
}

// gatedHostListener wraps the accepted Host connection so transport reads can stop at a synchronized barrier.
type gatedHostListener struct {
	// Listener accepts the real TCP connection used by gRPC.
	net.Listener
	// accepted reports the wrapped connection to the test.
	accepted chan *gatedconn.Conn
}

// Open keeps the Host connected and either receives the graceful close or deliberately stops receiving.
func (h *clientCloseHost) Open(stream extensionpb.ExtensionService_OpenServer) error {
	close(h.connected)
	if !h.receive {
		<-h.release
		return nil
	}
	for {
		request, err := stream.Recv()
		if err != nil {
			return err
		}
		if request.GetClose() != nil {
			close(h.closeReceived)
			return nil
		}
	}
}

// releaseOpen allows a non-receiving Host handler to return during test cleanup.
func (h *clientCloseHost) releaseOpen() { h.releaseOnce.Do(func() { close(h.release) }) }

// SendMsg observes the production writer's raw gRPC send without changing its result or ordering.
func (s *clientSendObserver) SendMsg(message any) error {
	request, observed := message.(*extensionpb.OpenRequest)
	observed = observed && request.GetOperationId() == blockedClientOperationID
	if observed {
		s.targetPending.Store(true)
		s.targetOnce.Do(func() { close(s.targetStarted) })
	}
	err := s.ClientStream.SendMsg(message)
	if observed {
		s.targetPending.Store(false)
		s.targetResult <- err
	}
	return err
}

// Accept wraps one real Host connection with a synchronized transport-read gate.
func (l *gatedHostListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := gatedconn.New(connection)
	l.accepted <- wrapped
	return wrapped, nil
}

// TestConnectionCloseCancelsBlockedRawSendAfterGrace verifies bounded graceful close over real gRPC.
func TestConnectionCloseCancelsBlockedRawSendAfterGrace(t *testing.T) {
	t.Parallel()

	// Arrange a connected Host that stops application and transport reads after Open starts.
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gatedListener := &gatedHostListener{Listener: listener, accepted: make(chan *gatedconn.Conn, 1)}
	host := &clientCloseHost{
		UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
		receive:                             false,
		connected:                           make(chan struct{}),
		closeReceived:                       make(chan struct{}),
		release:                             make(chan struct{}),
		releaseOnce:                         sync.Once{},
	}
	grpcServer := grpc.NewServer(
		grpc.InitialWindowSize(blockedClientTCPBufferSize),
		grpc.InitialConnWindowSize(blockedClientTCPBufferSize),
	)
	extensionpb.RegisterExtensionServiceServer(grpcServer, host)
	serveResult := make(chan error, 1)
	go func() { serveResult <- grpcServer.Serve(gatedListener) }()
	t.Cleanup(func() {
		host.releaseOpen()
		grpcServer.Stop()
		serveErr := <-serveResult
		assert.True(t, serveErr == nil || errors.Is(serveErr, grpc.ErrServerStopped), "gRPC Serve error: %v", serveErr)
	})

	targetStarted := make(chan struct{})
	targetResult := make(chan error, 1)
	var observer *clientSendObserver
	transport, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(blockedClientTCPBufferSize),
		grpc.WithInitialConnWindowSize(blockedClientTCPBufferSize),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			connection, dialErr := new(net.Dialer).DialContext(ctx, "tcp", address)
			if dialErr != nil {
				return nil, dialErr
			}
			if tcpConnection, ok := connection.(*net.TCPConn); ok {
				if bufferErr := tcpConnection.SetWriteBuffer(blockedClientTCPBufferSize); bufferErr != nil {
					return nil, errors.Join(bufferErr, connection.Close())
				}
			}
			return connection, nil
		}),
		grpc.WithStreamInterceptor(func(
			ctx context.Context,
			description *grpc.StreamDesc,
			connection *grpc.ClientConn,
			method string,
			streamer grpc.Streamer,
			options ...grpc.CallOption,
		) (grpc.ClientStream, error) {
			stream, streamErr := streamer(ctx, description, connection, method, options...)
			if streamErr != nil {
				return nil, streamErr
			}
			observer = &clientSendObserver{
				ClientStream:  stream,
				targetStarted: targetStarted,
				targetResult:  targetResult,
				targetOnce:    sync.Once{},
				targetPending: atomic.Bool{},
			}
			return observer, nil
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, transport.Close()) })
	connection, err := (&Client{
		process:   nil,
		service:   extensionpb.NewExtensionServiceClient(transport),
		done:      nil,
		version:   ProtocolVersion,
		closeOnce: sync.Once{},
	}).Open(t.Context())
	require.NoError(t, err)
	accepted := awaitBlockedClientSignal(t, gatedListener.accepted, "Host TCP connection was not accepted")
	awaitBlockedClientSignal(t, host.connected, "Host Open handler did not connect")
	accepted.BlockReads()
	t.Cleanup(accepted.ReleaseReads)
	targetID := strings.Repeat("x", blockedClientPayloadSize)
	for index := range blockedClientRequestCount {
		operationID := strings.Repeat("queued-send-", index+1)
		if index == 1 {
			operationID = blockedClientOperationID
		}
		_, err = connection.Cancel(t.Context(), operationID, targetID)
		require.NoError(t, err)
	}
	awaitBlockedClientSignal(t, targetStarted, "raw client Send did not start")
	awaitBlockedClientSignal(t, accepted.Blocked(), "Host transport read did not reach its closed gate")
	require.True(t, observer.targetPending.Load(), "raw client Send returned before the blocked state was observed")
	select {
	case sendErr := <-targetResult:
		require.FailNowf(t, "raw client Send returned while the Host stayed connected", "error: %v", sendErr)
	default:
	}

	// Act: close normally without Host receive, cancellation, connection close, or server stop assistance.
	closeResult := make(chan error, 2)
	startedAt := time.Now()
	go func() { closeResult <- connection.Close() }()
	go func() { closeResult <- connection.Close() }()
	firstCloseErr := awaitBlockedClientSignal(t, closeResult, "Connection.Close did not cancel the blocked raw Send")
	secondCloseErr := awaitBlockedClientSignal(t, closeResult, "concurrent Connection.Close did not observe cleanup")
	elapsed := time.Since(startedAt)

	// Assert timeout cancellation joins all transport work before every Close caller returns.
	require.ErrorIs(t, firstCloseErr, errConnectionCloseGraceTimeout)
	require.ErrorIs(t, secondCloseErr, errConnectionCloseGraceTimeout)
	assert.Equal(t, firstCloseErr.Error(), secondCloseErr.Error())
	require.ErrorIs(t, connection.CompletionFailures(), errConnectionCloseGraceTimeout)
	require.ErrorIs(t, context.Cause(connection.ctx), errConnectionCloseGraceTimeout)
	assert.GreaterOrEqual(t, elapsed, time.Second)
	assert.False(t, observer.targetPending.Load())
	require.Error(t, awaitBlockedClientSignal(t, targetResult, "raw client Send did not return before Close"))
	select {
	case <-host.closeReceived:
		require.FailNow(t, "non-receiving Host unexpectedly received CloseConnection")
	default:
	}
}

// TestConnectionCloseCompletesGracefullyWithResponsiveHost verifies the normal close control over real gRPC.
func TestConnectionCloseCompletesGracefullyWithResponsiveHost(t *testing.T) {
	t.Parallel()

	// Arrange a connected Host that receives and acknowledges stream closure by returning.
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host := &clientCloseHost{
		UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
		receive:                             true,
		connected:                           make(chan struct{}),
		closeReceived:                       make(chan struct{}),
		release:                             make(chan struct{}),
		releaseOnce:                         sync.Once{},
	}
	grpcServer := grpc.NewServer()
	extensionpb.RegisterExtensionServiceServer(grpcServer, host)
	serveResult := make(chan error, 1)
	go func() { serveResult <- grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		serveErr := <-serveResult
		assert.True(t, serveErr == nil || errors.Is(serveErr, grpc.ErrServerStopped), "gRPC Serve error: %v", serveErr)
	})
	transport, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, transport.Close()) })
	connection, err := (&Client{
		process:   nil,
		service:   extensionpb.NewExtensionServiceClient(transport),
		done:      nil,
		version:   ProtocolVersion,
		closeOnce: sync.Once{},
	}).Open(t.Context())
	require.NoError(t, err)
	awaitBlockedClientSignal(t, host.connected, "Host Open handler did not connect")

	// Act: close normally while the Host processes the graceful close protocol.
	closeResult := make(chan error, 1)
	go func() { closeResult <- connection.Close() }()
	closeErr := awaitBlockedClientSignal(t, closeResult, "responsive Host close did not complete")

	// Assert CloseConnection was delivered and close completed without an error.
	awaitBlockedClientSignal(t, host.closeReceived, "responsive Host did not receive CloseConnection")
	require.NoError(t, closeErr)
}

// awaitBlockedClientSignal waits only as a watchdog for a required synchronized test event.
func awaitBlockedClientSignal[T any](t *testing.T, signal <-chan T, message string) T {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(blockedClientWatchdogTimeout):
		require.FailNow(t, message)
		var zero T
		return zero
	}
}
