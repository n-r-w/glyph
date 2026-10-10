//go:build integration

package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

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

// canceledInitializationSuite checks shared cleanup grace through the real UI transport.
type canceledInitializationSuite struct {
	// Suite supplies the scenario's test context and lifecycle.
	suite.Suite
}

// TestCanceledBlockedInitializationUsesOneGrace runs an isolated cancellation suite in parallel.
func TestCanceledBlockedInitializationUsesOneGrace(t *testing.T) {
	// Arrange parallel execution for this isolated suite.
	t.Parallel()
	// Act through the suite's canceled initialization scenario.
	// Assert the one-grace cleanup guarantees within the suite scenario.
	suite.Run(t, new(canceledInitializationSuite))
}

// TestUsesOneGrace checks shared cleanup grace after a successful initialization Send.
func (s *canceledInitializationSuite) TestUsesOneGrace() {
	// t owns this suite scenario's context and cleanup.
	t := s.T()

	// Arrange an unread peer: the first large Send can queue, but leaves insufficient quota for the cancellation Send.
	peerStarted := make(chan struct{})
	peerStopped := make(chan struct{})
	peer := &blockedInitializationPeer{
		UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
		started:                      peerStarted,
		stopped:                      peerStopped,
	}
	stream, cancelStream, stopServer := openHostUITestStream(t, peer)
	defer stopServer()
	// initializationSent reports completion of the initial raw Send.
	initializationSent := make(chan error, 1)
	// cancellationStarted reports Send entry after the matcher accepts the cancellation request identity.
	cancellationStarted := make(chan struct{}, 1)
	// cancellationReturned reports completion of the cancellation raw Send.
	cancellationReturned := make(chan error, 1)
	receiveStarted := make(chan struct{})
	// receiveReturned reports completion of the initialization raw Recv.
	receiveReturned := make(chan error, 1)
	// closeReceiveReturned reports completion of the close-drain raw Recv.
	closeReceiveReturned := make(chan error, 1)
	// closeSendReturned reports completion of the raw CloseSend.
	closeSendReturned := make(chan error, 1)
	// initializationRequest is published to the test by initializationSent after the raw Send returns.
	var initializationRequest *uiv1.OpenRequest
	// activeSends and activeReceives count raw calls whose transport work has not returned.
	var activeSends, activeReceives atomic.Int64
	// concurrentReceives and closeOverlappedSend record forbidden transport overlap.
	var concurrentReceives, closeOverlappedSend atomic.Bool
	writerCause := errors.New("independent initialization writer failure")
	receiveCause := errors.New("independent initialization receive failure")
	// closeReceiveCause identifies the independent close-drain receive failure.
	closeReceiveCause := errors.New("independent initialization close receive failure")
	// closeSendCause identifies the independent CloseSend failure.
	closeSendCause := errors.New("independent initialization CloseSend failure")
	// observed records transport calls while delegating to the real stream.
	observed := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](gomock.NewController(t))
	observed.EXPECT().Context().Return(stream.Context()).AnyTimes()
	// initializationSend orders the cancellation Send after the initialization Send.
	initializationSend := observed.EXPECT().Send(gomock.Cond(func(request *uiv1.OpenRequest) bool {
		return request.GetOperationId() == initializationOperationID && request.GetRequest().GetInitialize() != nil
	})).DoAndReturn(func(request *uiv1.OpenRequest) error {
		activeSends.Add(1)
		initializationRequest = request
		// err reports the initial raw Send result.
		err := stream.Send(request)
		activeSends.Add(-1)
		initializationSent <- err
		return err
	})
	observed.EXPECT().Send(gomock.Cond(func(request *uiv1.OpenRequest) bool {
		return request.GetOperationId() == initializationCancellationOperationID &&
			request.GetRequest().GetCancel().GetTargetOperationId() == initializationOperationID
	})).After(initializationSend).DoAndReturn(func(request *uiv1.OpenRequest) error {
		activeSends.Add(1)
		cancellationStarted <- struct{}{}
		// err includes the raw cancellation Send result and the independent writer cause.
		err := errors.Join(stream.Send(request), writerCause)
		activeSends.Add(-1)
		cancellationReturned <- err
		return err
	})
	// initialReceive orders the close-drain Recv after the initialization Recv.
	initialReceive := observed.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
		if activeReceives.Add(1) != 1 {
			concurrentReceives.Store(true)
		}
		close(receiveStarted)
		// response and err report the raw initialization Recv result.
		response, err := stream.Recv()
		err = errors.Join(err, receiveCause)
		activeReceives.Add(-1)
		receiveReturned <- err
		return response, err
	})
	observed.EXPECT().Recv().After(initialReceive).DoAndReturn(func() (*uiv1.OpenResponse, error) {
		if activeReceives.Add(1) != 1 {
			concurrentReceives.Store(true)
		}
		// response and err report the raw close-drain Recv result.
		response, err := stream.Recv()
		err = errors.Join(err, closeReceiveCause)
		activeReceives.Add(-1)
		closeReceiveReturned <- err
		return response, err
	})
	observed.EXPECT().CloseSend().DoAndReturn(func() error {
		if activeSends.Load() != 0 {
			closeOverlappedSend.Store(true)
			cancelStream()
		}
		// err includes the raw CloseSend result and the independent close cause.
		err := errors.Join(stream.CloseSend(), closeSendCause)
		closeSendReturned <- err
		return err
	})
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
	defer cancelCaller(context.Canceled)
	result := make(chan error, 1)
	go func() { result <- service.Initialize(ctx, testInitialization()) }()
	awaitHostUISignal(t, peerStarted, "unread initialization peer start")
	require.NoError(t, awaitHostUIResult(t, initializationSent, "initialization Send queue submission"))
	// The serialized request exceeds both the initial local quota and the peer's entire stream window.
	require.Greater(t, proto.Size(initializationRequest), blockedInitializationRequestSize)
	awaitHostUISignal(t, receiveStarted, "initialization raw Recv start")
	require.Zero(t, activeSends.Load())
	require.EqualValues(t, 1, activeReceives.Load())

	// Act through caller cancellation after successful local submission of the initialization request.
	canceledAt := time.Now()
	cancelCaller(callerCause)
	awaitHostUISignal(t, cancellationStarted, "cancellation Send start")
	// The unread peer's 64 KiB windows cannot replenish the quota consumed by the 2 MiB first message.
	// The identified cancellation request owns this second Send.
	require.EqualValues(t, 1, activeSends.Load())
	require.EqualValues(t, 1, activeReceives.Load())
	select {
	// err reports an early cancellation Send completion.
	case err := <-cancellationReturned:
		t.Fatalf("cancellation Send returned before grace cancellation: %v", err)
	default:
	}
	select {
	// err reports an early initialization Recv completion.
	case err := <-receiveReturned:
		t.Fatalf("initialization Recv returned before grace cancellation: %v", err)
	default:
	}
	select {
	case <-peerStopped:
		t.Fatal("actual RPC stopped before the initialization grace elapsed")
	default:
	}
	var initializationErr error
	select {
	case initializationErr = <-result:
	case <-time.After(time.Until(canceledAt.Add(hostUIInitializationReturnBound))):
		assert.Fail(t, "Service.Initialize did not return after its one-second grace")
		cancelStream()
		_ = awaitHostUIResult(t, result, "forced blocked initialization cleanup")
		return
	}

	// Assert one shared grace joined every owned call and exposed each acquired error's complete text.
	require.GreaterOrEqual(t, time.Since(canceledAt), unsuccessfulInitializationGracePeriod)
	require.ErrorIs(t, initializationErr, context.DeadlineExceeded)
	require.ErrorContains(t, initializationErr, "UI unsuccessful initialization cleanup timed out after 1s")
	// cause identifies each required caller or transport failure.
	for _, cause := range []error{callerCause, writerCause, receiveCause, closeReceiveCause, closeSendCause} {
		require.ErrorIs(t, initializationErr, cause)
		require.ErrorContains(t, initializationErr, cause.Error())
	}
	// name labels the raw call whose completion channel is returned.
	for name, returned := range map[string]<-chan error{
		"cancellation Send":   cancellationReturned,
		"initialization Recv": receiveReturned,
		"close Recv":          closeReceiveReturned,
		"CloseSend":           closeSendReturned,
	} {
		select {
		// err reports the completed raw call failure.
		case err := <-returned:
			require.Error(t, err, name)
		default:
			t.Errorf("Service.Initialize returned before %s joined", name)
		}
	}
	assert.Zero(t, activeSends.Load())
	assert.Zero(t, activeReceives.Load())
	assert.False(t, concurrentReceives.Load())
	assert.False(t, closeOverlappedSend.Load())
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
