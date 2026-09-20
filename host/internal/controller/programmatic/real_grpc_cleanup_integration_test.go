//go:build integration

package programmatic

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/gatedconn"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// grpcCleanupLargePayloadSize exceeds the configured HTTP/2 flow-control windows.
	grpcCleanupLargePayloadSize = 3 << 20
	// grpcCleanupLargeMessageThreshold identifies intentionally flow-controlled responses.
	grpcCleanupLargeMessageThreshold = 1 << 20
	// grpcCleanupWatchdogLimit only bounds a failed test and is not behavioral evidence.
	grpcCleanupWatchdogLimit = 5 * time.Second
	// grpcCleanupOperationID identifies the operation that exhausts the local delivery queue.
	grpcCleanupOperationID = "queue-overflow"
	// grpcCleanupPendingSourceText identifies the source attached to the pending raw send.
	grpcCleanupPendingSourceText = "pending raw send source"
	// grpcCleanupOperationSourceText identifies the independent operation cleanup source.
	grpcCleanupOperationSourceText = "queue failure operation source"
	// grpcCleanupWriterSourceText identifies another acquired queued writer source.
	grpcCleanupWriterSourceText = "queued writer source"
)

// grpcCleanupSendObservation records one large raw transport send.
type grpcCleanupSendObservation struct {
	// returned receives the raw SendMsg result.
	returned chan error
	// returnOrder records raw send completion relative to handler completion.
	returnOrder atomic.Int64
}

// grpcCleanupListener limits the real TCP send buffer for deterministic backpressure.
type grpcCleanupListener struct {
	// Listener accepts the real local TCP connection.
	net.Listener
	// accepted closes after the server connection has its small send buffer.
	accepted chan struct{}
}

// Accept configures one real TCP connection without replacing its I/O behavior.
func (l *grpcCleanupListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tcpConnection, ok := connection.(*net.TCPConn); ok {
		if err = tcpConnection.SetWriteBuffer(1024); err != nil {
			_ = connection.Close()
			return nil, err
		}
	}
	close(l.accepted)
	return connection, nil
}

// grpcCleanupTracker records production handler and raw transport boundaries.
type grpcCleanupTracker struct {
	// largeSendStarted receives every observed large raw SendMsg invocation.
	largeSendStarted chan *grpcCleanupSendObservation
	// secondReceiveStarted closes when the nested raw receive starts after the request.
	secondReceiveStarted chan struct{}
	// secondReceiveReturned receives the nested raw receive result.
	secondReceiveReturned chan error
	// handlerReturned receives the production handler result.
	handlerReturned chan error
	// receiveCalls counts raw request receives.
	receiveCalls atomic.Int64
	// sequence orders handler and raw transport completion observations.
	sequence atomic.Int64
	// handlerReturnOrder records production handler completion.
	handlerReturnOrder atomic.Int64
	// secondReceiveReturnOrder records nested raw receive completion.
	secondReceiveReturnOrder atomic.Int64
}

// newGRPCCleanupTracker creates synchronization channels for one RPC.
func newGRPCCleanupTracker() *grpcCleanupTracker {
	return &grpcCleanupTracker{
		largeSendStarted:         make(chan *grpcCleanupSendObservation, 128),
		secondReceiveStarted:     make(chan struct{}),
		secondReceiveReturned:    make(chan error, 1),
		handlerReturned:          make(chan error, 1),
		receiveCalls:             atomic.Int64{},
		sequence:                 atomic.Int64{},
		handlerReturnOrder:       atomic.Int64{},
		secondReceiveReturnOrder: atomic.Int64{},
	}
}

// grpcCleanupServerStream observes raw gRPC calls without replacing the real transport.
type grpcCleanupServerStream struct {
	// ServerStream is the real local TCP gRPC stream.
	grpc.ServerStream
	// tracker owns synchronization for the observed RPC.
	tracker *grpcCleanupTracker
}

// SendMsg records each intentionally large response around the real transport call.
func (s *grpcCleanupServerStream) SendMsg(message any) error {
	var observation *grpcCleanupSendObservation
	if wireMessage, ok := message.(proto.Message); ok && proto.Size(wireMessage) >= grpcCleanupLargeMessageThreshold {
		observation = &grpcCleanupSendObservation{returned: make(chan error, 1), returnOrder: atomic.Int64{}}
		s.tracker.largeSendStarted <- observation
	}
	err := s.ServerStream.SendMsg(message)
	if observation != nil {
		observation.returnOrder.Store(s.tracker.sequence.Add(1))
		observation.returned <- err
	}
	return err
}

// RecvMsg records the nested raw receive that the outer receive loop does not join.
func (s *grpcCleanupServerStream) RecvMsg(message any) error {
	call := s.tracker.receiveCalls.Add(1)
	if call == 2 {
		close(s.tracker.secondReceiveStarted)
	}
	err := s.ServerStream.RecvMsg(message)
	if call == 2 {
		s.tracker.secondReceiveReturnOrder.Store(s.tracker.sequence.Add(1))
		s.tracker.secondReceiveReturned <- err
	}
	return err
}

// grpcCleanupRequest creates one valid GetModels request.
func grpcCleanupRequest() *programmaticv1.OpenRequest {
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(grpcCleanupOperationID)
	payload := new(programmaticv1.ControllerRequest)
	payload.SetGetModels(new(programmaticv1.GetModels))
	request.SetRequest(payload)
	return request
}

// grpcCleanupProgress creates one valid agent-start progress event.
func grpcCleanupProgress() OperationProgress {
	return OperationProgress{
		AgentEvent: mo.Some(AgentEvent{
			OperationID:  grpcCleanupOperationID,
			Type:         AgentEventAgentStart,
			RunID:        "queue-item",
			ModelContent: mo.None[ModelContent](), ToolCallPreview: mo.None[ToolCallPreview](),
			FinalToolCall: mo.None[FinalToolCall](), Retry: mo.None[RetryProgress](),
			ToolExecution: mo.None[ToolExecution](),
			ToolProgress:  mo.None[ToolProgress](), ToolResult: mo.None[ToolResult](),
			ModelResponse: mo.None[ModelResponse](), Turn: mo.None[TurnSummary](),
			Agent: mo.None[AgentSummary](),
		}),
		TreeNavigation:      mo.None[TreeNavigationProgress](),
		TreeNavigationRetry: mo.None[RetryProgress](),
		CompactionStage:     mo.None[string](),
	}
}

// grpcCleanupAwait receives one synchronized result or fails through the test-only watchdog.
func grpcCleanupAwait[T any](t *testing.T, result <-chan T, description string) T {
	t.Helper()
	timer := time.NewTimer(grpcCleanupWatchdogLimit)
	defer timer.Stop()
	select {
	case value := <-result:
		return value
	case <-timer.C:
		t.Fatalf("watchdog expired while waiting for %s", description)
		var zero T
		return zero
	}
}

// grpcCleanupAwaitSignal waits for one synchronized event through the test-only watchdog.
func grpcCleanupAwaitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	grpcCleanupAwait(t, signal, description)
}

// TestFatalCleanupReturnsBeforeUnreadRawTransport verifies logical cleanup on a real gRPC stream.
func TestFatalCleanupReturnsBeforeUnreadRawTransport(t *testing.T) {
	t.Parallel()

	// Arrange admitted work, a real local gRPC stream, and explicit transport observation.
	controller := gomock.NewController(t)
	operationStarted := make(chan struct{})
	fillQueue := make(chan struct{})
	queueFailure := make(chan error, 1)
	operationSource := errors.New(grpcCleanupOperationSourceText)
	pendingSource := errors.New(grpcCleanupPendingSourceText)
	writerSource := errors.New(grpcCleanupWriterSourceText)
	prepared := operationmock.NewMockOperationPrepared[OperationProgress, Response](controller)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, reporter operation.Reporter[OperationProgress]) operation.Outcome[Response] {
			close(operationStarted)
			<-fillQueue
			for {
				reportErr := reporter.Report(grpcCleanupProgress())
				if reportErr != nil {
					queueFailure <- reportErr
					return operation.Failed[Response](FailureCodeInternal, operationSource)
				}
			}
		},
	)
	prepared.EXPECT().Release()
	host := NewMockHostSession(controller)
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
	writerBound := make(chan *operation.Writer[*programmaticv1.OpenResponse], 1)
	output := NewMockConnectionOutput(controller)
	output.EXPECT().BindWriter(gomock.Any()).DoAndReturn(
		func(writer *operation.Writer[*programmaticv1.OpenResponse]) func() {
			writerBound <- writer
			return func() {}
		},
	)
	service := New(t.Context(), host, output)
	tracker := newGRPCCleanupTracker()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gatedListener := &grpcCleanupListener{Listener: listener, accepted: make(chan struct{})}
	server := grpc.NewServer(
		grpc.WriteBufferSize(0),
		grpc.StaticStreamWindowSize(1024),
		grpc.StaticConnWindowSize(1024),
		grpc.StreamInterceptor(func(
			srv any,
			stream grpc.ServerStream,
			_ *grpc.StreamServerInfo,
			handler grpc.StreamHandler,
		) error {
			handlerErr := handler(srv, &grpcCleanupServerStream{ServerStream: stream, tracker: tracker})
			tracker.handlerReturnOrder.Store(tracker.sequence.Add(1))
			tracker.handlerReturned <- handlerErr
			return handlerErr
		}),
	)
	programmaticv1.RegisterProgrammaticControlServiceServer(server, service)
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(gatedListener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = gatedListener.Close()
		<-serveResult
	})
	clientConnection := gatedconn.New(nil)
	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithReadBufferSize(0),
		grpc.WithStaticStreamWindowSize(1024),
		grpc.WithStaticConnWindowSize(1024),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			connection, dialErr := new(net.Dialer).DialContext(ctx, "tcp", address)
			if dialErr != nil {
				return nil, dialErr
			}
			clientConnection.Conn = connection
			return clientConnection, nil
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		clientConnection.ReleaseReads()
		_ = connection.Close()
	})
	clientContext, cancelClient := context.WithCancel(t.Context())
	t.Cleanup(cancelClient)
	client := programmaticv1.NewProgrammaticControlServiceClient(connection)
	stream, err := client.Open(clientContext)
	require.NoError(t, err)
	grpcCleanupAwaitSignal(t, gatedListener.accepted, "server connection")
	writer := grpcCleanupAwait(t, writerBound, "writer binding")
	require.NoError(t, stream.Send(grpcCleanupRequest()))
	grpcCleanupAwaitSignal(t, operationStarted, "operation admission")
	grpcCleanupAwaitSignal(t, tracker.secondReceiveStarted, "nested raw receive")
	clientConnection.BlockReads()
	require.NoError(t, writer.Enqueue(new(programmaticv1.OpenResponse), nil))
	grpcCleanupAwaitSignal(t, clientConnection.Blocked(), "client transport read gate")

	// Act after the unread peer and small TCP buffer exhaust real transport flow control.
	largeResponse := new(programmaticv1.OpenResponse)
	largeResponse.SetOperationId(strings.Repeat("x", grpcCleanupLargePayloadSize))
	require.NoError(t, writer.Enqueue(largeResponse, errors.Join(pendingSource, writerSource)))
	firstSend := grpcCleanupAwait(t, tracker.largeSendStarted, "first large raw send start")
	require.NoError(t, grpcCleanupAwait(t, firstSend.returned, "first large raw send buffering"))
	require.NoError(t, writer.Enqueue(largeResponse, errors.Join(pendingSource, writerSource)))
	activeSend := grpcCleanupAwait(t, tracker.largeSendStarted, "flow-controlled raw send start")
	for {
		enqueueErr := writer.Enqueue(largeResponse, errors.Join(pendingSource, writerSource))
		if errors.Is(enqueueErr, operation.ErrQueueFull) {
			break
		}
		require.NoError(t, enqueueErr)
	}
	select {
	case sendErr := <-activeSend.returned:
		require.Failf(t, "large raw send returned before cleanup", "error: %v", sendErr)
	default:
	}
	close(fillQueue)
	localFailure := grpcCleanupAwait(t, queueFailure, "local queue failure")
	require.ErrorIs(t, localFailure, operation.ErrQueueFull)
	handlerErr := grpcCleanupAwait(t, tracker.handlerReturned, "production handler return")
	completion := grpcCleanupAwait(t, service.Completions(), "session completion")

	// Assert logical sources and classification are complete before raw transport termination.
	assert.Equal(t, codes.ResourceExhausted, status.Code(handlerErr))
	assert.Equal(t, SessionCompletionTransportFailure, completion.Cause)
	for _, result := range []error{handlerErr, completion.Err} {
		require.ErrorIs(t, result, operation.ErrQueueFull)
		require.ErrorContains(t, result, grpcCleanupPendingSourceText)
		require.ErrorContains(t, result, grpcCleanupOperationSourceText)
		require.ErrorContains(t, result, grpcCleanupWriterSourceText)
	}
	rawSendErr := grpcCleanupAwait(t, activeSend.returned, "raw send termination")
	rawReceiveErr := grpcCleanupAwait(t, tracker.secondReceiveReturned, "raw receive termination")
	assert.Equal(t, codes.Canceled, status.Code(rawSendErr))
	assert.Equal(t, codes.Canceled, status.Code(rawReceiveErr))
	assert.Positive(t, tracker.handlerReturnOrder.Load())
	assert.Greater(t, activeSend.returnOrder.Load(), tracker.handlerReturnOrder.Load())
	assert.Greater(t, tracker.secondReceiveReturnOrder.Load(), tracker.handlerReturnOrder.Load())
}
