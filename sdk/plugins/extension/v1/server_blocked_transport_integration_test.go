//go:build integration

package extensionv1

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
	"github.com/n-r-w/glyph/internal/testsupport/gatedconn"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// blockedExtensionPayloadSize is one bounded response used after client transport reads stop.
	blockedExtensionPayloadSize = 2 * 1024 * 1024
	// blockedExtensionOutputCount supplies one buffered Send and one backpressured Send.
	blockedExtensionOutputCount = 2
	// blockedExtensionTCPBufferSize limits bytes accepted by the server socket after reads stop.
	blockedExtensionTCPBufferSize = 4 * 1024
	// blockedExtensionCleanupTimeout bounds test watchdogs without changing production behavior.
	blockedExtensionCleanupTimeout = 5 * time.Second
)

// blockedExtensionService coordinates a queue failure while one real server Send is pending.
type blockedExtensionService struct {
	// sendProbe allows one response to move the client transport reader into its gate.
	sendProbe <-chan struct{}
	// probeQueued signals that the reader-gating response entered writer ownership.
	probeQueued chan<- struct{}
	// sendBlockedOutput allows bounded output after the client reader reaches its gate.
	sendBlockedOutput <-chan struct{}
	// blockedOutputQueued signals that bounded output entered writer ownership.
	blockedOutputQueued chan<- struct{}
	// fillQueue allows Execute to overflow the delivery queue after a rejection source is acquired.
	fillQueue <-chan struct{}
	// rejectionEnqueued signals that one source-bearing rejection is already queued.
	rejectionEnqueued chan<- struct{}
	// localFailure reports the delivery failure that initiates connection cleanup.
	localFailure chan<- error
	// executeReleaseStarted signals entry into owner Release before handler return.
	executeReleaseStarted chan<- struct{}
	// executeReleaseGate lets the test snapshot pending raw calls before Release returns.
	executeReleaseGate <-chan struct{}
	// executeReleased signals that owner work and Release bookkeeping completed.
	executeReleased chan<- struct{}
	// ownerSource is the independent source returned by the active Execute operation.
	ownerSource error
	// writerSource is attached to rejection output acquired by the writer.
	writerSource error
	// rejectionCalls counts sequential Execute preparations for the enqueue barrier.
	rejectionCalls atomic.Int64
	// rejectionOnce closes the enqueue barrier once.
	rejectionOnce sync.Once
}

// blockedExtensionRegister completes the registration required before Execute admission.
type blockedExtensionRegister struct{}

// blockedExtensionExecute produces output until the production delivery queue fails.
type blockedExtensionExecute struct {
	// service owns the synchronization and source errors for the operation.
	service *blockedExtensionService
}

// extensionTransportObserver records bounded Send and all raw Recv lifecycles.
type extensionTransportObserver struct {
	// ServerStream is the real gRPC transport stream.
	grpc.ServerStream
	// firstSendResult reports completion of the one Send accepted before backpressure.
	firstSendResult chan<- error
	// targetSendStarted signals entry into the following backpressured progress Send.
	targetSendStarted chan struct{}
	// targetSendResult reports the backpressured progress Send result separately from handler return.
	targetSendResult chan<- error
	// receiveResult reports raw receive failures after handler return.
	receiveResult chan<- error
	// targetSendOnce closes the target start signal once.
	targetSendOnce sync.Once
	// sendStarts counts bounded raw Send calls that started.
	sendStarts atomic.Int64
	// sendReturns counts bounded raw Send calls that returned.
	sendReturns atomic.Int64
	// receiveStarts counts raw receive calls that started.
	receiveStarts atomic.Int64
	// receiveReturns counts raw receive calls that returned.
	receiveReturns atomic.Int64
}

// extensionSmallBufferListener limits each accepted server TCP send buffer.
type extensionSmallBufferListener struct {
	// Listener accepts the real TCP connections used by gRPC.
	net.Listener
}

// PrepareRegister admits the one registration operation used by the test Host.
func (*blockedExtensionService) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (RegisterOperation, error) {
	return blockedExtensionRegister{}, nil
}

// PrepareHandle rejects the unused operation kind.
func (*blockedExtensionService) PrepareHandle(
	context.Context,
	*extensionpb.HandleRequest,
) (HandleOperation, error) {
	return nil, errors.New("Handle is not used")
}

// PrepareExecute admits the first operation and rejects later requests with a retained source.
func (service *blockedExtensionService) PrepareExecute(
	context.Context,
	*extensionpb.ExecuteRequest,
) (ExecuteOperation, error) {
	call := service.rejectionCalls.Add(1)
	if call == 1 {
		return &blockedExtensionExecute{service: service}, nil
	}
	if call == 3 {
		service.rejectionOnce.Do(func() { close(service.rejectionEnqueued) })
	}
	return nil, Reject(rejectionCodeNotReady, service.writerSource)
}

// Run returns the registration contract.
func (blockedExtensionRegister) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	return contractRegistration(), nil
}

// Release has no registration resources to release.
func (blockedExtensionRegister) Release() {}

// Run gates client reads, queues bounded output, and triggers the real writer queue failure.
func (operationUnderTest *blockedExtensionExecute) Run(
	ctx context.Context,
	reporter *ProgressReporter,
) (*extensionpb.ToolResult, error) {
	<-operationUnderTest.service.sendProbe
	probe := extensionpb.ToolProgress_builder{
		Channel: new(extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS),
		Content: new("reader gate probe"),
	}.Build()
	if err := reporter.Report(ctx, probe); err != nil {
		return nil, errors.Join(operationUnderTest.service.ownerSource, err)
	}
	close(operationUnderTest.service.probeQueued)

	<-operationUnderTest.service.sendBlockedOutput
	progress := extensionpb.ToolProgress_builder{
		Channel: new(extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS),
		Content: new(strings.Repeat("x", blockedExtensionPayloadSize)),
	}.Build()
	for range blockedExtensionOutputCount {
		if err := reporter.Report(ctx, progress); err != nil {
			operationUnderTest.service.localFailure <- err
			return nil, errors.Join(operationUnderTest.service.ownerSource, err)
		}
	}
	close(operationUnderTest.service.blockedOutputQueued)

	<-operationUnderTest.service.fillQueue
	for {
		if err := reporter.Report(ctx, progress); err != nil {
			operationUnderTest.service.localFailure <- err
			return nil, errors.Join(operationUnderTest.service.ownerSource, err)
		}
	}
}

// Release signals completion of owner work bookkeeping.
func (operationUnderTest *blockedExtensionExecute) Release() {
	close(operationUnderTest.service.executeReleaseStarted)
	<-operationUnderTest.service.executeReleaseGate
	close(operationUnderTest.service.executeReleased)
}

// SendMsg observes actual bounded raw Sends without replacing the real transport.
func (stream *extensionTransportObserver) SendMsg(message any) error {
	response, isResponse := message.(*extensionpb.OpenResponse)
	isBlockedProgress := isResponse && len(
		response.GetEvent().GetProgress().GetTool().GetContent(),
	) == blockedExtensionPayloadSize
	if !isBlockedProgress {
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
func (stream *extensionTransportObserver) RecvMsg(message any) error {
	stream.receiveStarts.Add(1)
	err := stream.ServerStream.RecvMsg(message)
	stream.receiveReturns.Add(1)
	if err != nil {
		stream.receiveResult <- err
	}
	return err
}

// Accept limits the server-side kernel send buffer for deterministic backpressure.
func (listener *extensionSmallBufferListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tcpConnection, ok := connection.(*net.TCPConn)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("accepted Extension test connection is not TCP")
	}
	if err = tcpConnection.SetWriteBuffer(blockedExtensionTCPBufferSize); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("set Extension test TCP send buffer: %w", err)
	}
	return connection, nil
}

// TestBlockedTransportFailureReturnsExtensionHandler verifies fatal cleanup without Host assistance.
func TestBlockedTransportFailureReturnsExtensionHandler(t *testing.T) {
	t.Parallel()

	// Arrange failure-safe operation gates and a real gRPC connection with controlled client reads.
	sendProbe := make(chan struct{})
	releaseSendProbe := sync.OnceFunc(func() { close(sendProbe) })
	sendBlockedOutput := make(chan struct{})
	releaseBlockedOutput := sync.OnceFunc(func() { close(sendBlockedOutput) })
	fillQueue := make(chan struct{})
	releaseFillQueue := sync.OnceFunc(func() { close(fillQueue) })
	executeReleaseGate := make(chan struct{})
	releaseExecute := sync.OnceFunc(func() { close(executeReleaseGate) })
	probeQueued := make(chan struct{})
	blockedOutputQueued := make(chan struct{})
	rejectionEnqueued := make(chan struct{})
	localFailure := make(chan error, 1)
	executeReleaseStarted := make(chan struct{})
	executeReleased := make(chan struct{})
	ownerSource := errors.New("extension owner cleanup source")
	writerSource := errors.New("extension acquired writer source")
	service := &blockedExtensionService{
		sendProbe: sendProbe, probeQueued: probeQueued,
		sendBlockedOutput: sendBlockedOutput, blockedOutputQueued: blockedOutputQueued,
		fillQueue: fillQueue, rejectionEnqueued: rejectionEnqueued, localFailure: localFailure,
		executeReleaseStarted: executeReleaseStarted, executeReleaseGate: executeReleaseGate,
		executeReleased: executeReleased, ownerSource: ownerSource, writerSource: writerSource,
		rejectionCalls: atomic.Int64{}, rejectionOnce: sync.Once{},
	}
	firstSendResult := make(chan error, 1)
	targetSendStarted := make(chan struct{})
	targetSendResult := make(chan error, 1)
	receiveResult := make(chan error, 1)
	handlerResult := make(chan error, 1)
	var observedStream *extensionTransportObserver
	var clientTransport *gatedconn.Conn
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	grpcListener := &extensionSmallBufferListener{Listener: listener}
	grpcServer := grpc.NewServer(
		grpc.InitialWindowSize(1024),
		grpc.InitialConnWindowSize(1024),
		grpc.StreamInterceptor(func(
			srv any,
			serverStream grpc.ServerStream,
			_ *grpc.StreamServerInfo,
			handler grpc.StreamHandler,
		) error {
			observedStream = &extensionTransportObserver{
				ServerStream: serverStream, firstSendResult: firstSendResult,
				targetSendStarted: targetSendStarted, targetSendResult: targetSendResult,
				receiveResult: receiveResult, targetSendOnce: sync.Once{},
				sendStarts: atomic.Int64{}, sendReturns: atomic.Int64{},
				receiveStarts: atomic.Int64{}, receiveReturns: atomic.Int64{},
			}
			handlerErr := handler(srv, observedStream)
			handlerResult <- handlerErr
			return handlerErr
		}),
	)
	extensionpb.RegisterExtensionServiceServer(grpcServer, newServer(service))
	serveResult := make(chan error, 1)
	go func() { serveResult <- grpcServer.Serve(grpcListener) }()
	t.Cleanup(func() {
		releaseSendProbe()
		releaseBlockedOutput()
		releaseFillQueue()
		releaseExecute()
		if clientTransport != nil {
			clientTransport.ReleaseReads()
		}
		grpcServer.Stop()
		if serveErr := <-serveResult; serveErr != nil {
			assert.ErrorIs(t, serveErr, grpc.ErrServerStopped)
		}
	})
	transportReady := make(chan *gatedconn.Conn, 1)
	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			rawConnection, dialErr := new(net.Dialer).DialContext(ctx, "tcp", address)
			if dialErr != nil {
				return nil, dialErr
			}
			gatedConnection := gatedconn.New(rawConnection)
			transportReady <- gatedConnection
			return gatedConnection, nil
		}),
		grpc.WithInitialWindowSize(1024),
		grpc.WithInitialConnWindowSize(1024),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	stream, err := extensionpb.NewExtensionServiceClient(connection).Open(t.Context())
	require.NoError(t, err)
	clientTransport = awaitBlockedExtensionSignal(t, transportReady, "client TCP transport")
	require.NoError(t, stream.Send(integrationRegisterRequest("register")))
	for range 3 {
		receiveHostDisconnectResponse(t, stream, "Register lifecycle")
	}
	require.NoError(t, stream.Send(integrationExecuteRequest("execute")))
	for range 2 {
		receiveHostDisconnectResponse(t, stream, "Execute startup lifecycle")
	}

	// Gate client reads with one probe, then queue bounded output and an acquired writer source.
	clientTransport.BlockReads()
	releaseSendProbe()
	awaitBlockedExtensionSignal(t, probeQueued, "reader-gate probe enqueue")
	awaitBlockedExtensionSignal(t, clientTransport.Blocked(), "client transport read gate")
	releaseBlockedOutput()
	awaitBlockedExtensionSignal(t, blockedOutputQueued, "bounded output enqueue")
	firstSendErr := awaitBlockedExtensionSignal(t, firstSendResult, "first bounded raw Send")
	require.NoError(t, firstSendErr)
	awaitBlockedExtensionSignal(t, targetSendStarted, "backpressured raw Send start")
	require.NoError(t, stream.Send(integrationExecuteRequest("rejected-1")))
	require.NoError(t, stream.Send(integrationExecuteRequest("rejected-2")))
	awaitBlockedExtensionSignal(t, rejectionEnqueued, "writer source enqueue")
	require.Eventually(t, func() bool {
		return observedStream.receiveStarts.Load() > observedStream.receiveReturns.Load()
	}, blockedExtensionCleanupTimeout, time.Millisecond, "raw server Recv did not become pending")

	// Act through the actual local delivery queue failure.
	releaseFillQueue()
	failureErr := awaitBlockedExtensionSignal(t, localFailure, "local delivery failure")
	require.ErrorIs(t, failureErr, operation.ErrQueueFull)
	require.Greater(t, observedStream.sendStarts.Load(), observedStream.sendReturns.Load())
	require.Greater(t, observedStream.receiveStarts.Load(), observedStream.receiveReturns.Load())
	awaitBlockedExtensionSignal(t, executeReleaseStarted, "Execute Release start")
	require.Greater(t, observedStream.sendStarts.Load(), observedStream.sendReturns.Load())
	require.Greater(t, observedStream.receiveStarts.Load(), observedStream.receiveReturns.Load())
	releaseExecute()
	awaitBlockedExtensionSignal(t, executeReleased, "Execute Release")

	// Assert logical cleanup and error collection reach handler return before peer assistance.
	handlerErr := awaitBlockedExtensionSignal(t, handlerResult, "Extension handler return")
	require.Error(t, handlerErr)
	assert.Equal(t, codes.ResourceExhausted, status.Code(handlerErr))
	require.ErrorIs(t, handlerErr, ownerSource)
	require.ErrorIs(t, handlerErr, writerSource)
	require.ErrorIs(t, handlerErr, operation.ErrQueueFull)

	// Assert handler return separately releases raw calls without adding their result to the report.
	sendErr := awaitBlockedExtensionSignal(t, targetSendResult, "raw Send cancellation")
	recvErr := awaitBlockedExtensionSignal(t, receiveResult, "raw Recv cancellation")
	require.Error(t, sendErr)
	assert.Equal(t, codes.Canceled, status.Code(sendErr))
	assert.Equal(t, codes.Canceled, status.Code(recvErr))
	assert.NotContains(t, handlerErr.Error(), sendErr.Error())
	clientTransport.ReleaseReads()
	var clientErr error
	for clientErr == nil {
		_, clientErr = stream.Recv()
	}
	require.Error(t, clientErr)
	assert.Equal(t, codes.ResourceExhausted, status.Code(clientErr))
}

// awaitBlockedExtensionSignal waits for one explicitly synchronized test stage.
func awaitBlockedExtensionSignal[T any](t *testing.T, signal <-chan T, stage string) T {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(blockedExtensionCleanupTimeout):
		t.Fatalf("%s did not complete", stage)
		var zero T
		return zero
	}
}
