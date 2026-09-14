//go:build integration

package uiv1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// blockedUIPayloadSize is one bounded request used after client transport reads stop.
	blockedUIPayloadSize = 2 * 1024 * 1024
	// blockedUIRequestCount supplies one buffered Send and one backpressured Send.
	blockedUIRequestCount = 2
	// blockedUITCPBufferSize limits bytes accepted by the server socket after reads stop.
	blockedUITCPBufferSize = 4 * 1024
	// blockedUICleanupTimeout bounds observable logical cleanup without changing production behavior.
	blockedUICleanupTimeout = 2 * time.Second
	// blockedUIWriterSource identifies rejection output retained by writer bookkeeping.
	blockedUIWriterSource = "UI acquired writer source"
)

// blockedUIService starts one large Host request and retains controlled cleanup sources.
type blockedUIService struct {
	// sendProbe allows one response to move the client transport reader into its gate.
	sendProbe <-chan struct{}
	// probeQueued signals that the reader-gating response entered writer ownership.
	probeQueued chan<- struct{}
	// sendBlockedOutput allows bounded output after the client reader reaches its gate.
	sendBlockedOutput <-chan struct{}
	// runStarted signals that bounded Host requests entered writer ownership.
	runStarted chan<- struct{}
	// fillWriter allows Service.Run to trigger the actual outbound queue failure.
	fillWriter <-chan struct{}
	// localFailure reports the queue failure returned by Host.Start.
	localFailure chan<- error
	// runStopped signals that Service.Run completed its failure path.
	runStopped chan<- struct{}
	// closeStarted signals entry into Service.Close before handler return can cancel transport.
	closeStarted chan<- struct{}
	// closeGate lets the test snapshot pending raw calls before Service.Close returns.
	closeGate <-chan struct{}
	// closeFinished signals completion of Service.Close.
	closeFinished chan<- struct{}
	// runSource is retained from logical application cleanup.
	runSource error
	// closeSource is retained from service cleanup.
	closeSource error
}

// blockedUIInitialize completes the initialization required before Service.Run.
type blockedUIInitialize struct{}

// uiSmallBufferListener limits each accepted server TCP send buffer.
type uiSmallBufferListener struct {
	// Listener accepts the real TCP connections used by gRPC.
	net.Listener
}

// uiGatedReadConn stops client transport reads at an explicit test barrier.
type uiGatedReadConn struct {
	// Conn is the real client TCP connection.
	net.Conn
	// gated selects whether new reads wait for release.
	gated atomic.Bool
	// blocked signals that a client transport read reached the gate.
	blocked chan struct{}
	// release allows gated reads to continue during normal or failed cleanup.
	release chan struct{}
	// blockedOnce closes the blocked signal once.
	blockedOnce sync.Once
	// releaseOnce closes the release signal once.
	releaseOnce sync.Once
}

// uiTransportObserver records target Send and all raw Recv lifecycles.
type uiTransportObserver struct {
	// ServerStream is the real gRPC transport stream.
	grpc.ServerStream
	// firstSendResult reports completion of the one Send accepted before backpressure.
	firstSendResult chan<- error
	// targetSendStarted signals entry into the following backpressured Host-request Send.
	targetSendStarted chan struct{}
	// targetSendResult reports the backpressured Send result separately from handler return.
	targetSendResult chan error
	// receiveResult reports raw receive failures after handler return.
	receiveResult chan error
	// targetSendOnce closes the target start signal once.
	targetSendOnce sync.Once
	// sendStarts counts large raw send calls that started.
	sendStarts atomic.Int64
	// sendReturns counts large raw send calls that returned.
	sendReturns atomic.Int64
	// receiveStarts counts raw receive calls that started.
	receiveStarts atomic.Int64
	// receiveReturns counts raw receive calls that returned.
	receiveReturns atomic.Int64
}

// PrepareInitialize admits the one initialization operation used by the test Host.
func (*blockedUIService) PrepareInitialize(
	context.Context,
	*uipb.Initialization,
) (InitializeOperation, error) {
	return blockedUIInitialize{}, nil
}

// Run starts bounded transport output, then triggers the actual outbound queue failure.
func (service *blockedUIService) Run(ctx context.Context, host *Host) error {
	<-service.sendProbe
	probeRequest := new(uipb.UIRequest)
	probeRequest.SetSubmit(uipb.SubmitCommand_builder{Text: new("reader gate probe")}.Build())
	if _, err := host.Start(ctx, "reader-gate-probe", probeRequest); err != nil {
		return errors.Join(service.runSource, err)
	}
	close(service.probeQueued)
	<-service.sendBlockedOutput
	largeRequest := new(uipb.UIRequest)
	largeRequest.SetSubmit(uipb.SubmitCommand_builder{
		Text: new(strings.Repeat("x", blockedUIPayloadSize)),
	}.Build())
	for index := range blockedUIRequestCount {
		if _, err := host.Start(ctx, fmt.Sprintf("blocked-output-%d", index), largeRequest); err != nil {
			return errors.Join(service.runSource, err)
		}
	}
	close(service.runStarted)
	<-service.fillWriter
	fillRequest := new(uipb.UIRequest)
	fillRequest.SetSubmit(uipb.SubmitCommand_builder{Text: new("fill writer queue")}.Build())
	for index := blockedUIRequestCount; ; index++ {
		if _, err := host.Start(ctx, fmt.Sprintf("fill-output-%d", index), fillRequest); err != nil {
			service.localFailure <- err
			close(service.runStopped)
			return errors.Join(service.runSource, err)
		}
	}
}

// Close reports a distinct source after all logical connection work is joined.
func (service *blockedUIService) Close() error {
	close(service.closeStarted)
	<-service.closeGate
	close(service.closeFinished)
	return service.closeSource
}

// Run returns successful UI initialization.
func (blockedUIInitialize) Run(context.Context) (*uipb.Initialized, error) {
	return new(uipb.Initialized), nil
}

// Release has no initialization resources to release.
func (blockedUIInitialize) Release() {}

// SendMsg observes the actual large raw Send without replacing the real transport.
func (stream *uiTransportObserver) SendMsg(message any) error {
	response, isResponse := message.(*uipb.OpenResponse)
	isLargeRequest := isResponse && len(response.GetRequest().GetSubmit().GetText()) == blockedUIPayloadSize
	if !isLargeRequest {
		return stream.ServerStream.SendMsg(message)
	}
	ordinal := stream.sendStarts.Add(1)
	if ordinal == 2 {
		stream.targetSendOnce.Do(func() { close(stream.targetSendStarted) })
	}
	err := stream.ServerStream.SendMsg(message)
	stream.sendReturns.Add(1)
	if ordinal == 1 {
		stream.firstSendResult <- err
	}
	if ordinal == 2 {
		stream.targetSendResult <- err
	}
	return err
}

// RecvMsg records the actual raw receive call separately from its outer worker.
func (stream *uiTransportObserver) RecvMsg(message any) error {
	stream.receiveStarts.Add(1)
	err := stream.ServerStream.RecvMsg(message)
	stream.receiveReturns.Add(1)
	if err != nil {
		stream.receiveResult <- err
	}
	return err
}

// Accept limits the server-side kernel send buffer for deterministic backpressure.
func (listener *uiSmallBufferListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tcpConnection, ok := connection.(*net.TCPConn)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("accepted UI test connection is not TCP")
	}
	if err = tcpConnection.SetWriteBuffer(blockedUITCPBufferSize); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("set UI test TCP send buffer: %w", err)
	}
	return connection, nil
}

// Read waits at the configured client transport barrier before reading more TCP bytes.
func (connection *uiGatedReadConn) Read(buffer []byte) (int, error) {
	if connection.gated.Load() {
		connection.blockedOnce.Do(func() { close(connection.blocked) })
		<-connection.release
	}
	return connection.Conn.Read(buffer)
}

// blockReads makes the next client transport Read wait at the gate.
func (connection *uiGatedReadConn) blockReads() {
	connection.gated.Store(true)
}

// releaseReads unblocks transport reads exactly once for failure-safe cleanup.
func (connection *uiGatedReadConn) releaseReads() {
	connection.releaseOnce.Do(func() { close(connection.release) })
}

// Close releases a gated read before closing the real client connection.
func (connection *uiGatedReadConn) Close() error {
	connection.releaseReads()
	return connection.Conn.Close()
}

// TestBlockedTransportFailureReturnsUIServerHandler controls the existing context-aware send path.
func TestBlockedTransportFailureReturnsUIServerHandler(t *testing.T) {
	t.Parallel()

	// Arrange failure-safe gates and a real gRPC connection with controlled client reads.
	sendProbe := make(chan struct{})
	releaseSendProbe := sync.OnceFunc(func() { close(sendProbe) })
	sendBlockedOutput := make(chan struct{})
	releaseBlockedOutput := sync.OnceFunc(func() { close(sendBlockedOutput) })
	fillWriter := make(chan struct{})
	releaseFillWriter := sync.OnceFunc(func() { close(fillWriter) })
	localFailure := make(chan error, 1)
	runStarted := make(chan struct{})
	runStopped := make(chan struct{})
	closeStarted := make(chan struct{})
	closeGate := make(chan struct{})
	releaseClose := sync.OnceFunc(func() { close(closeGate) })
	closeFinished := make(chan struct{})
	runSource := errors.New("UI logical run cleanup source")
	closeSource := errors.New("UI service close source")
	probeQueued := make(chan struct{})
	service := &blockedUIService{
		sendProbe: sendProbe, probeQueued: probeQueued, sendBlockedOutput: sendBlockedOutput,
		runStarted: runStarted, fillWriter: fillWriter, localFailure: localFailure, runStopped: runStopped,
		closeStarted: closeStarted, closeGate: closeGate, closeFinished: closeFinished,
		runSource: runSource, closeSource: closeSource,
	}
	firstSendResult := make(chan error, 1)
	targetSendStarted := make(chan struct{})
	targetSendResult := make(chan error, 1)
	receiveResult := make(chan error, 1)
	handlerResult := make(chan error, 1)
	var observedStream *uiTransportObserver
	var clientTransport *uiGatedReadConn
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcListener := &uiSmallBufferListener{Listener: listener}
	grpcServer := grpc.NewServer(
		grpc.InitialWindowSize(1024),
		grpc.InitialConnWindowSize(1024),
		grpc.StreamInterceptor(func(
			srv any,
			serverStream grpc.ServerStream,
			_ *grpc.StreamServerInfo,
			handler grpc.StreamHandler,
		) error {
			observedStream = &uiTransportObserver{
				ServerStream:      serverStream,
				firstSendResult:   firstSendResult,
				targetSendStarted: targetSendStarted,
				targetSendResult:  targetSendResult,
				receiveResult:     receiveResult,
				targetSendOnce:    sync.Once{},
				sendStarts:        atomic.Int64{},
				sendReturns:       atomic.Int64{},
				receiveStarts:     atomic.Int64{},
				receiveReturns:    atomic.Int64{},
			}
			handlerErr := handler(srv, observedStream)
			handlerResult <- handlerErr
			return handlerErr
		}),
	)
	uipb.RegisterUIServiceServer(grpcServer, newServer(service))
	serveResult := make(chan error, 1)
	go func() { serveResult <- grpcServer.Serve(grpcListener) }()
	t.Cleanup(func() {
		releaseSendProbe()
		releaseBlockedOutput()
		releaseFillWriter()
		releaseClose()
		if clientTransport != nil {
			clientTransport.releaseReads()
		}
		grpcServer.Stop()
		if serveErr := <-serveResult; serveErr != nil {
			assert.ErrorIs(t, serveErr, grpc.ErrServerStopped)
		}
	})
	transportReady := make(chan *uiGatedReadConn, 1)
	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			rawConnection, dialErr := new(net.Dialer).DialContext(ctx, "tcp", address)
			if dialErr != nil {
				return nil, dialErr
			}
			gatedConnection := &uiGatedReadConn{
				Conn: rawConnection, gated: atomic.Bool{}, blocked: make(chan struct{}),
				release: make(chan struct{}), blockedOnce: sync.Once{}, releaseOnce: sync.Once{},
			}
			transportReady <- gatedConnection
			return gatedConnection, nil
		}),
		grpc.WithInitialWindowSize(1024),
		grpc.WithInitialConnWindowSize(1024),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	stream, err := uipb.NewUIServiceClient(connection).Open(t.Context())
	require.NoError(t, err)
	clientTransport = awaitBlockedUISignal(t, transportReady, "client TCP transport")
	sendIntegrationInitialization(t, stream)
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	clientTransport.blockReads()
	releaseSendProbe()
	awaitBlockedUISignal(t, probeQueued, "reader-gate probe enqueue")
	awaitBlockedUISignal(t, clientTransport.blocked, "client transport read gate")
	releaseBlockedOutput()
	awaitBlockedUISignal(t, runStarted, "UI Service.Run start")
	firstSendErr := awaitBlockedUISignal(t, firstSendResult, "first bounded raw Send")
	require.NoError(t, firstSendErr)
	awaitBlockedUISignal(t, targetSendStarted, "backpressured raw Send start")

	// Queue one retained source, establish a pending raw Recv, then trigger local writer failure.
	cancelRequest := new(uipb.HostRequest)
	cancelRequest.SetCancel(operationv1.CancelOperation_builder{
		TargetOperationId: new(blockedUIWriterSource),
	}.Build())
	baselineReceiveStarts := observedStream.receiveStarts.Load()
	for _, operationID := range []string{"rejected-1", "rejected-2"} {
		require.NoError(t, stream.Send(uipb.OpenRequest_builder{
			OperationId: new(operationID), Request: cancelRequest,
			Event: nil, ConnectionEvent: nil, Close: nil,
		}.Build()))
	}
	require.Eventually(t, func() bool {
		return observedStream.receiveStarts.Load() >= baselineReceiveStarts+2 &&
			observedStream.receiveStarts.Load() > observedStream.receiveReturns.Load()
	}, blockedUICleanupTimeout, time.Millisecond, "source enqueue receive barrier did not complete")
	releaseFillWriter()
	failureErr := awaitBlockedUISignal(t, localFailure, "UI local writer queue failure")
	require.ErrorIs(t, failureErr, operation.ErrQueueFull)
	require.Greater(t, observedStream.receiveStarts.Load(), observedStream.receiveReturns.Load())
	require.Greater(t, observedStream.sendStarts.Load(), observedStream.sendReturns.Load())
	awaitBlockedUISignal(t, runStopped, "UI Service.Run cleanup")
	awaitBlockedUISignal(t, closeStarted, "UI Service.Close start")
	require.Greater(t, observedStream.receiveStarts.Load(), observedStream.receiveReturns.Load())
	require.Greater(t, observedStream.sendStarts.Load(), observedStream.sendReturns.Load())
	releaseClose()
	awaitBlockedUISignal(t, closeFinished, "UI Service.Close")

	// Assert current production cleanup retains logical and writer sources before handler return.
	handlerErr := awaitBlockedUISignal(t, handlerResult, "UI handler return")
	require.Error(t, handlerErr)
	assert.Equal(t, codes.ResourceExhausted, status.Code(handlerErr))
	require.ErrorIs(t, handlerErr, runSource)
	require.ErrorIs(t, handlerErr, closeSource)
	require.ErrorIs(t, handlerErr, operation.ErrQueueFull)
	require.ErrorContains(t, handlerErr, blockedUIWriterSource)

	// Assert handler return separately releases raw calls without adding their result to the report.
	sendErr := awaitBlockedUITransportError(t, targetSendResult, "raw Send cancellation")
	recvErr := awaitBlockedUISignal(t, receiveResult, "raw Recv cancellation")
	assert.Equal(t, codes.Canceled, status.Code(sendErr))
	assert.Equal(t, codes.Canceled, status.Code(recvErr))
	assert.NotContains(t, handlerErr.Error(), sendErr.Error())
	clientTransport.releaseReads()
	var clientErr error
	for range blockedUIRequestCount + 2 {
		if _, clientErr = stream.Recv(); clientErr != nil {
			break
		}
	}
	require.Error(t, clientErr)
	assert.Equal(t, codes.ResourceExhausted, status.Code(clientErr))
}

// awaitBlockedUISignal waits for one explicitly synchronized test stage.
func awaitBlockedUISignal[T any](t *testing.T, signal <-chan T, stage string) T {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(blockedUICleanupTimeout):
		t.Fatalf("%s did not complete", stage)
		var zero T
		return zero
	}
}

// awaitBlockedUITransportError skips successful buffered sends and returns the transport failure.
func awaitBlockedUITransportError(t *testing.T, signal <-chan error, stage string) error {
	t.Helper()
	for {
		if err := awaitBlockedUISignal(t, signal, stage); err != nil {
			return err
		}
	}
}
