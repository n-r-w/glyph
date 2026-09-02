//go:build !integration

package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestRunOperationsTransportFailurePreservesCauseAndJoinsWork verifies receive and send failure coordination.
func TestRunOperationsTransportFailurePreservesCauseAndJoinsWork(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		sendFails  bool
		expected   codes.Code
		sourceText string
		sendErr    error
	}{
		{
			name: "receive", sendFails: false, expected: codes.DataLoss,
			sourceText: "UI receive transport failed", sendErr: nil,
		},
		{
			name: "send status", sendFails: true, expected: codes.Unavailable,
			sourceText: "UI send transport failed", sendErr: status.Error(codes.Unavailable, "UI send transport failed"),
		},
		{
			name: "send plain error", sendFails: true, expected: codes.Unavailable,
			sourceText: "plain UI send failed", sendErr: errors.New("plain UI send failed"),
		},
		{
			name: "queue overflow", sendFails: true, expected: codes.ResourceExhausted,
			sourceText: operation.ErrQueueFull.Error(), sendErr: operation.ErrQueueFull,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one active prepared operation and one transport failure.
			controller := gomock.NewController(t)
			stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
			streamContext, cancelStream := context.WithCancel(t.Context())
			stream.EXPECT().Context().Return(streamContext).AnyTimes()
			request := operationRequest("active", "work")
			started := make(chan struct{})
			stopped := make(chan struct{})
			released := make(chan struct{})
			prepared := operation.NewMockPrepared[domainui.Frame, domainui.Frame](controller)
			prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
				ctx context.Context,
				reporter operation.Reporter[domainui.Frame],
			) operation.Outcome[domainui.Frame] {
				close(started)
				if test.sendFails {
					_ = reporter.Report(testLifecycleFrame())
				}
				<-ctx.Done()
				close(stopped)
				return operation.Canceled[domainui.Frame]()
			})
			prepared.EXPECT().Release().Do(func() { close(released) })
			stream.EXPECT().Recv().Return(request, nil)
			if test.sendFails {
				stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
					<-streamContext.Done()
					return nil, context.Cause(streamContext)
				})
				var sends atomic.Int32
				stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(*uiv1.OpenRequest) error {
					if sends.Add(1) == 3 {
						return test.sendErr
					}
					return nil
				})
			} else {
				stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
					<-started
					return nil, status.Error(test.expected, test.sourceText)
				})
				stream.EXPECT().Send(gomock.Any()).AnyTimes().Return(nil)
			}
			stream.EXPECT().CloseSend().DoAndReturn(func() error {
				cancelStream()
				return nil
			})
			_, cancelChannel := context.WithCancel(t.Context())
			transport := &channel{
				stream: stream, cancel: cancelChannel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
				writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
				failConnection: nil,
			}

			// Act through the complete adapter coordinator.
			err := transport.RunOperations(t.Context(), func() {}, func(
				context.Context,
				domainui.Command,
			) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
				return prepared, nil
			})

			// Assert status, complete text, stopped work, and Release before return.
			require.Error(t, err)
			assert.Equal(t, test.expected, status.Code(err))
			assert.ErrorContains(t, err, test.sourceText)
			if test.sendErr != nil {
				assert.ErrorIs(t, err, test.sendErr)
			}
			select {
			case <-stopped:
			default:
				t.Fatal("RunOperations returned before active work stopped")
			}
			select {
			case <-released:
			default:
				t.Fatal("RunOperations returned before Release")
			}
		})
	}
}

// TestRunOperationsJoinsOperationAndTerminalTransportFailures verifies terminal provenance.
func TestRunOperationsJoinsOperationAndTerminalTransportFailures(t *testing.T) {
	t.Parallel()

	// Arrange one failed operation whose Failed terminal message cannot be sent.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	streamContext, cancelStream := context.WithCancel(t.Context())
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	source := errors.New("session persistence failed")
	transportCause := status.Error(codes.Unavailable, "terminal transport failed")
	released := make(chan struct{})
	prepared := operation.NewMockPrepared[domainui.Frame, domainui.Frame](controller)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		operation.Failed[domainui.Frame]("INTERNAL", source),
	)
	prepared.EXPECT().Release().Do(func() { close(released) })
	stream.EXPECT().Recv().Return(operationRequest("failed", "work"), nil)
	stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
		<-streamContext.Done()
		return nil, context.Cause(streamContext)
	})
	var sends atomic.Int32
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(*uiv1.OpenRequest) error {
		if sends.Add(1) == 3 {
			return transportCause
		}
		return nil
	})
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		cancelStream()
		return nil
	})
	_, cancelChannel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancelChannel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}

	// Act through terminal transport failure.
	err := transport.RunOperations(t.Context(), func() {}, func(
		context.Context,
		domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		return prepared, nil
	})

	// Assert operation and transport causes remain reachable after Release.
	require.Error(t, err)
	assert.ErrorIs(t, err, source)
	assert.ErrorIs(t, err, transportCause)
	select {
	case <-released:
	default:
		t.Fatal("RunOperations returned before failed operation Release")
	}
}

// TestRunOperationsRealQueueOverflowClosesTransportAndJoinsWork verifies blocked-writer failure coordination.
func TestRunOperationsRealQueueOverflowClosesTransportAndJoinsWork(t *testing.T) {
	t.Parallel()

	// Arrange one operation that fills the production writer queue behind a blocked Send.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	streamContext, cancelStream := context.WithCancel(t.Context())
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	overflow := make(chan struct{})
	stopped := make(chan struct{})
	released := make(chan struct{})
	prepared := operation.NewMockPrepared[domainui.Frame, domainui.Frame](controller)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
		ctx context.Context,
		reporter operation.Reporter[domainui.Frame],
	) operation.Outcome[domainui.Frame] {
		for {
			if err := reporter.Report(testLifecycleFrame()); err != nil {
				if errors.Is(err, operation.ErrQueueFull) {
					close(overflow)
				}
				<-ctx.Done()
				close(stopped)
				return operation.Canceled[domainui.Frame]()
			}
		}
	})
	prepared.EXPECT().Release().Do(func() { close(released) })
	stream.EXPECT().Recv().Return(operationRequest("overflow", "work"), nil)
	stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
		<-streamContext.Done()
		return nil, context.Cause(streamContext)
	})
	var sends atomic.Int32
	closeCalled := make(chan struct{})
	sendRelease := make(chan struct{})
	var releaseOnce sync.Once
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(*uiv1.OpenRequest) error {
		if sends.Add(1) <= 2 {
			return nil
		}
		<-sendRelease
		return context.Canceled
	})
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		close(closeCalled)
		releaseOnce.Do(func() { close(sendRelease) })
		cancelStream()
		return nil
	})
	_, cancelChannel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancelChannel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}
	result := make(chan error, 1)

	// Act through real queue saturation.
	go func() {
		result <- transport.RunOperations(t.Context(), func() {}, func(
			context.Context,
			domainui.Command,
		) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
			return prepared, nil
		})
	}()
	<-overflow

	// Assert transport closure starts before the blocked writer is joined.
	closedBeforeJoin := assert.Eventually(t, func() bool {
		select {
		case <-closeCalled:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond, "queue overflow did not close transport before waiting for writer")
	if !closedBeforeJoin {
		releaseOnce.Do(func() { close(sendRelease) })
	}
	err := <-result
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	assert.ErrorIs(t, err, operation.ErrQueueFull)
	select {
	case <-stopped:
	default:
		t.Fatal("RunOperations returned before overflow cancellation stopped work")
	}
	select {
	case <-released:
	default:
		t.Fatal("RunOperations returned before overflow Release")
	}
}

// TestRunOperationsRequestedClosePreservesCloseSendFailure verifies cleanup error visibility.
func TestRunOperationsRequestedClosePreservesCloseSendFailure(t *testing.T) {
	t.Parallel()

	// Arrange a canceled caller and a failing request-stream close.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	streamContext, cancelStream := context.WithCancel(t.Context())
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
		<-streamContext.Done()
		return nil, io.EOF
	})
	stream.EXPECT().Send(gomock.Any()).Return(nil)
	closeCause := errors.New("close request stream failed")
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		cancelStream()
		return closeCause
	})
	_, cancelChannel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancelChannel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through requested closure.
	err := transport.RunOperations(ctx, func() {}, func(
		context.Context,
		domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		return nil, errors.New("unexpected preparation")
	})

	// Assert requested cancellation does not hide the cleanup failure.
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.ErrorIs(t, err, closeCause)
	assert.ErrorContains(t, err, closeCause.Error())
}

// TestRunOperationsWriterFailureDuringCloseStillHalfClosesAndJoinsReceive verifies failed drain cleanup.
func TestRunOperationsWriterFailureDuringCloseStillHalfClosesAndJoinsReceive(t *testing.T) {
	t.Parallel()

	// Arrange writer failure after local close begins and a receiver blocked until CloseSend.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	streamContext, cancelStream := context.WithCancel(t.Context())
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	receiveJoined := make(chan struct{})
	stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
		<-streamContext.Done()
		close(receiveJoined)
		return nil, io.EOF
	})
	source := errors.New("close response send failed")
	stream.EXPECT().Send(gomock.Any()).Return(source)
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		cancelStream()
		return nil
	})
	_, cancelChannel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancelChannel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through requested close with failed close-message delivery.
	err := transport.RunOperations(ctx, func() {}, func(
		context.Context,
		domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		return nil, errors.New("unexpected preparation")
	})

	// Assert the source cause remains and receive joined after transport half-close.
	require.Error(t, err)
	assert.ErrorIs(t, err, source)
	select {
	case <-receiveJoined:
	default:
		t.Fatal("RunOperations returned before receive joined")
	}
}

// TestRunOperationsLocalCloseFailsNewRequestsBeforePeerClose verifies immediate admission stop.
func TestRunOperationsLocalCloseFailsNewRequestsBeforePeerClose(t *testing.T) {
	t.Parallel()

	// Arrange one late request after the Host sends CloseConnection but before peer close.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	streamContext, cancelStream := context.WithCancel(t.Context())
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	closeDelivered := make(chan struct{})
	stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
		<-closeDelivered
		return operationRequest("late-local", "work"), nil
	})
	sent := make(chan *uiv1.OpenRequest, 1)
	var closeOnce sync.Once
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(request *uiv1.OpenRequest) error {
		sent <- request
		if request.GetClose() != nil {
			closeOnce.Do(func() { close(closeDelivered) })
		}
		return nil
	})
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		cancelStream()
		return nil
	})
	_, cancelChannel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancelChannel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through locally requested closure.
	err := transport.RunOperations(ctx, func() {}, func(
		context.Context,
		domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		t.Fatal("late request reached preparation after local close")
		return nil, nil
	})

	// Assert the late request fails the stream instead of producing lifecycle rejection.
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "connection is closing")
	assert.NotNil(t, (<-sent).GetClose())
}

// TestRunOperationsPeerCloseFailsNewRequests verifies Host closure protocol enforcement.
func TestRunOperationsPeerCloseFailsNewRequests(t *testing.T) {
	t.Parallel()

	// Arrange peer CloseConnection followed by a late request and response EOF.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	closeResponse := new(uiv1.OpenResponse)
	closeResponse.SetClose(new(operationv1.CloseConnection))
	stream.EXPECT().Recv().Return(closeResponse, nil)
	stream.EXPECT().Recv().Return(operationRequest("late", "work"), nil)
	sent := make(chan *uiv1.OpenRequest, 2)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(request *uiv1.OpenRequest) error {
		sent <- request
		return nil
	})
	stream.EXPECT().CloseSend().Return(nil)
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}

	// Act through peer-requested closure.
	err := transport.RunOperations(t.Context(), func() {}, func(
		context.Context,
		domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		t.Fatal("late request reached preparation after close")
		return nil, nil
	})

	// Assert peer close is accepted but the later request fails the stream.
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "connection is closing")
	assert.NotNil(t, (<-sent).GetClose())
}

// operationRequest creates one valid submit request envelope.
func operationRequest(id, text string) *uiv1.OpenResponse {
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new(text)}.Build())
	return uiv1.OpenResponse_builder{OperationId: new(id), Request: request, Event: nil, Close: nil}.Build()
}
