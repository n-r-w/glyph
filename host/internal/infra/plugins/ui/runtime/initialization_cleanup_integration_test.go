//go:build integration

package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// blockedInitializationRequestSize exceeds the bounded HTTP/2 receive window.
	blockedInitializationRequestSize = 2 * 1024 * 1024
	// hostUIInitializationReturnBound asserts return after the one-second production grace.
	hostUIInitializationReturnBound = 1500 * time.Millisecond
)

// blockedInitializationPeer keeps the RPC open without reading or sending application messages.
type blockedInitializationPeer struct {
	uiv1.UnimplementedUIServiceServer
	// started reports that the real server handler owns the RPC without reading.
	started chan<- struct{}
	// stopped reports that actual RPC cancellation released the handler.
	stopped chan<- struct{}
}

// responsiveCancellationPeer completes initialization cancellation and the close handshake.
type responsiveCancellationPeer struct {
	uiv1.UnimplementedUIServiceServer
	// initializationReceived reports receipt of the initial request.
	initializationReceived chan<- struct{}
	// cancellationReceived reports receipt of the ordered cancellation request.
	cancellationReceived chan<- struct{}
	// closeReceived reports receipt of the graceful CloseConnection request.
	closeReceived chan<- struct{}
	// stopped reports graceful handler completion.
	stopped chan<- struct{}
}

// Open keeps the peer application idle until the Host cancels the actual RPC.
func (peer *blockedInitializationPeer) Open(
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
) error {
	close(peer.started)
	<-stream.Context().Done()
	close(peer.stopped)
	return stream.Context().Err()
}

// Open completes the target, cancellation, and connection-close lifecycles in order.
func (peer *responsiveCancellationPeer) Open(
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
) error {
	request, err := stream.Recv()
	if err != nil {
		return err
	}
	if request.GetRequest().GetInitialize() == nil {
		return errors.New("expected initialization request")
	}
	close(peer.initializationReceived)
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	for _, event := range []*uiv1.OpenResponse{
		uiLifecycleResponse(accepted),
		uiLifecycleResponse(running),
	} {
		if err = stream.Send(event); err != nil {
			return err
		}
	}
	request, err = stream.Recv()
	if err != nil {
		return err
	}
	if request.GetOperationId() != initializationCancellationOperationID ||
		request.GetRequest().GetCancel().GetTargetOperationId() != initializationOperationID {
		return errors.New("expected ordered initialization cancellation request")
	}
	close(peer.cancellationReceived)
	canceled := new(uiv1.UIEvent)
	canceled.SetCanceled(new(operationv1.Canceled))
	cancelCompleted := new(uiv1.UICompleted)
	cancelCompleted.SetCancel(operationv1.CancelCompleted_builder{
		TargetState: new(operationv1.TerminalState_TERMINAL_STATE_CANCELED),
	}.Build())
	completed := new(uiv1.UIEvent)
	completed.SetCompleted(cancelCompleted)
	for _, event := range []*uiv1.OpenResponse{
		uiLifecycleResponse(canceled),
		uiCancellationLifecycleResponse(accepted),
		uiCancellationLifecycleResponse(running),
		uiCancellationLifecycleResponse(completed),
	} {
		if err = stream.Send(event); err != nil {
			return err
		}
	}
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

// TestCanceledBlockedInitializationUsesOneGrace verifies bounded cancellation with blocked real transport calls.
func TestCanceledBlockedInitializationUsesOneGrace(t *testing.T) {
	t.Parallel()

	// Arrange a real peer that does not read and a production initialization larger than flow-control windows.
	peerStarted := make(chan struct{})
	peerStopped := make(chan struct{})
	peer := &blockedInitializationPeer{
		UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
		started:                      peerStarted,
		stopped:                      peerStopped,
	}
	stream, cancelStream, stopServer := openHostUITestStream(t, peer)
	defer stopServer()
	sendStarted := make(chan struct{})
	sendReturned := make(chan struct{})
	receiveStarted := make(chan struct{})
	receiveReturned := make(chan struct{})
	writerCause := errors.New("independent initialization writer failure")
	receiveCause := errors.New("independent initialization receive failure")
	observed := &observedHostUIClientStream{
		UIService_OpenClient: stream,
		sendStarted:          sendStarted,
		sendStartedOnce:      sync.Once{},
		sendReturned:         sendReturned,
		sendReturnedOnce:     sync.Once{},
		receiveStarted:       receiveStarted,
		receiveStartedOnce:   sync.Once{},
		receiveReturned:      receiveReturned,
		receiveReturnedOnce:  sync.Once{},
		activeSends:          atomic.Int64{},
		activeReceives:       atomic.Int64{},
		concurrentReceives:   atomic.Bool{},
		closeOverlappedSend:  atomic.Bool{},
		sendErr:              writerCause,
		receiveErr:           receiveCause,
		cancel:               cancelStream,
	}
	startupCause := errors.New("large initialization startup source")
	largeStartupCause := fmt.Errorf("%s: %w", strings.Repeat("x", blockedInitializationRequestSize), startupCause)
	service := New()
	service.stream = observed
	service.cancel = cancelStream
	service.startupReport = startup.LoadReport{
		Issues:     []startup.Issue{{PluginIDs: nil, Path: "", Err: largeStartupCause}},
		Extensions: nil,
	}
	callerCause := errors.New("caller canceled blocked initialization")
	ctx, cancelCaller := context.WithCancelCause(t.Context())
	result := make(chan error, 1)
	go func() { result <- service.Initialize(ctx, testInitialization()) }()
	awaitHostUISignal(t, peerStarted, "blocked initialization peer start")
	awaitHostUISignal(t, sendStarted, "raw initialization Send start")
	awaitHostUISignal(t, receiveStarted, "raw initialization Recv start")
	require.EqualValues(t, 1, observed.activeSends.Load())
	require.EqualValues(t, 1, observed.activeReceives.Load())
	select {
	case <-sendReturned:
		t.Fatal("raw initialization Send returned before caller cancellation")
	default:
	}
	select {
	case <-receiveReturned:
		t.Fatal("raw initialization Recv returned before caller cancellation")
	default:
	}

	// Act through caller cancellation while both raw calls remain active.
	canceledAt := time.Now()
	cancelCaller(callerCause)
	select {
	case <-peerStopped:
		t.Fatal("actual RPC stopped before the initialization grace elapsed")
	default:
	}
	var initializationErr error
	returnedWithinBound := true
	select {
	case initializationErr = <-result:
	case <-time.After(hostUIInitializationReturnBound):
		assert.Fail(t, "Service.Initialize did not return after its one-second grace")
		returnedWithinBound = false
		cancelStream()
		initializationErr = awaitHostUIResult(t, result, "forced blocked initialization cleanup")
	}
	if !returnedWithinBound {
		awaitHostUISignal(t, peerStopped, "forced blocked initialization peer cleanup")
		return
	}

	// Assert timeout cancellation joined both original calls and retained every acquired cause.
	require.GreaterOrEqual(t, time.Since(canceledAt), unsuccessfulInitializationGracePeriod)
	require.ErrorIs(t, initializationErr, callerCause)
	require.ErrorIs(t, initializationErr, context.DeadlineExceeded)
	require.ErrorContains(t, initializationErr, "UI unsuccessful initialization cleanup timed out after 1s")
	require.ErrorIs(t, initializationErr, writerCause)
	require.ErrorIs(t, initializationErr, receiveCause)
	select {
	case <-sendReturned:
	default:
		t.Error("Service.Initialize returned before raw Send joined")
	}
	select {
	case <-receiveReturned:
	default:
		t.Error("Service.Initialize returned before raw Recv joined")
	}
	assert.Zero(t, observed.activeSends.Load())
	assert.Zero(t, observed.activeReceives.Load())
	assert.False(t, observed.concurrentReceives.Load())
	assert.False(t, observed.closeOverlappedSend.Load())
	awaitHostUISignal(t, peerStopped, "blocked initialization peer cleanup")
}

// TestCanceledInitializationCompletesResponsiveHandshake verifies grace preserves orderly peer cleanup.
func TestCanceledInitializationCompletesResponsiveHandshake(t *testing.T) {
	t.Parallel()

	// Arrange a real peer that completes cancellation and then accepts CloseConnection.
	initializationReceived := make(chan struct{})
	cancellationReceived := make(chan struct{})
	closeReceived := make(chan struct{})
	peerStopped := make(chan struct{})
	peer := &responsiveCancellationPeer{
		UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
		initializationReceived:       initializationReceived,
		cancellationReceived:         cancellationReceived,
		closeReceived:                closeReceived,
		stopped:                      peerStopped,
	}
	stream, cancelStream, stopServer := openHostUITestStream(t, peer)
	defer stopServer()
	service := New()
	service.stream = stream
	service.cancel = cancelStream
	callerCause := errors.New("caller canceled responsive initialization")
	ctx, cancelCaller := context.WithCancelCause(t.Context())
	result := make(chan error, 1)
	go func() { result <- service.Initialize(ctx, testInitialization()) }()
	awaitHostUISignal(t, initializationReceived, "responsive initialization receipt")

	// Act through caller cancellation while the peer remains responsive.
	cancelCaller(callerCause)
	initializationErr := awaitHostUIResult(t, result, "responsive initialization cancellation")

	// Assert ordered cancellation and close complete without consuming the grace timeout.
	require.ErrorIs(t, initializationErr, callerCause)
	require.NotErrorIs(t, initializationErr, context.DeadlineExceeded)
	require.NotContains(t, initializationErr.Error(), "timed out")
	awaitHostUISignal(t, cancellationReceived, "responsive initialization cancellation receipt")
	awaitHostUISignal(t, closeReceived, "responsive CloseConnection receipt")
	awaitHostUISignal(t, peerStopped, "responsive peer handler cleanup")
}
