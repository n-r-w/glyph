//go:build !integration

package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

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

// TestChannelRejectsOrdinaryRequestBeforeReadiness verifies the direct Host contract NOT_READY result.
func TestChannelRejectsOrdinaryRequestBeforeReadiness(t *testing.T) {
	t.Parallel()

	// Arrange initialization with one ordinary request interleaved before UI startup completes.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	ordinary := new(uiv1.UIRequest)
	ordinary.SetSubmit(uiv1.SubmitCommand_builder{Text: new("hello")}.Build())
	early := uiv1.OpenResponse_builder{
		OperationId: new("early"), Request: ordinary, Event: nil, Close: nil,
	}.Build()
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	completedPayload := new(uiv1.UICompleted)
	completedPayload.SetInitialized(new(uiv1.Initialized))
	completed := new(uiv1.UIEvent)
	completed.SetCompleted(completedPayload)
	stream.EXPECT().Send(gomock.Any()).Return(nil)
	stream.EXPECT().Recv().Return(early, nil)
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(message *uiv1.OpenRequest) error {
		rejected := message.GetEvent().GetRejected()
		require.NotNil(t, rejected)
		assert.Equal(t, rejectionCodeNotReady, rejected.GetCode())
		assert.Equal(t, "host UI is not ready", rejected.GetMessage())
		return nil
	})
	stream.EXPECT().Recv().Return(uiLifecycleResponse(accepted), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(completed), nil)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream:           stream,
		cancel:           cancel,
		closed:           atomic.Bool{},
		mutex:            sync.Mutex{},
		ready:            false,
		writer:           nil,
		progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false, failConnection: nil,
	}
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	initialization := uiv1.OpenRequest_builder{
		OperationId: new(initializationOperationID), Request: request, Event: nil,
		ConnectionEvent: nil, Close: nil,
	}.Build()

	// Act through direct initialization receipt.
	err := transport.initialize(t.Context(), initialization)

	// Assert startup continues after rejecting the early ordinary request.
	require.NoError(t, err)
	assert.True(t, transport.ready)
}

// TestStartupCancellationRejectionCategories verifies cancellation rejection while initialization continues.
func TestStartupCancellationRejectionCategories(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		target  string
		code    string
		message string
	}{
		{
			name: "empty target", target: "", code: rejectionCodeInvalidArgument,
			message: "UI cancellation target is required",
		},
		{
			name: "unowned target", target: "missing", code: rejectionCodeTargetNotActive,
			message: "UI cancellation target \"missing\" is not active",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an initialization stream that receives one early cancellation request.
			controller := gomock.NewController(t)
			stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
			stream.EXPECT().Context().Return(t.Context()).AnyTimes()
			cancelRequest := new(uiv1.UIRequest)
			cancelRequest.SetCancel(operationv1.CancelOperation_builder{
				TargetOperationId: new(test.target),
			}.Build())
			earlyCancellation := uiv1.OpenResponse_builder{
				OperationId: new("early-cancel"), Request: cancelRequest, Event: nil, Close: nil,
			}.Build()
			accepted := new(uiv1.UIEvent)
			accepted.SetAccepted(new(operationv1.Accepted))
			running := new(uiv1.UIEvent)
			running.SetRunning(new(operationv1.Running))
			completedPayload := new(uiv1.UICompleted)
			completedPayload.SetInitialized(new(uiv1.Initialized))
			completed := new(uiv1.UIEvent)
			completed.SetCompleted(completedPayload)
			rejectionSent := make(chan struct{})
			gomock.InOrder(
				stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *uiv1.OpenRequest) error {
					assert.Equal(t, initializationOperationID, request.GetOperationId())
					assert.NotNil(t, request.GetRequest().GetInitialize())
					return nil
				}),
				stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *uiv1.OpenRequest) error {
					assert.Equal(t, "early-cancel", request.GetOperationId())
					rejected := request.GetEvent().GetRejected()
					require.NotNil(t, rejected)
					assert.Equal(t, test.code, rejected.GetCode())
					assert.Equal(t, test.message, rejected.GetMessage())
					close(rejectionSent)
					return nil
				}),
			)
			gomock.InOrder(
				stream.EXPECT().Recv().Return(earlyCancellation, nil),
				stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
					<-rejectionSent
					return uiLifecycleResponse(accepted), nil
				}),
				stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil),
				stream.EXPECT().Recv().Return(uiLifecycleResponse(completed), nil),
			)
			_, cancel := context.WithCancel(t.Context())
			transport := &channel{
				stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: false,
				writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
				failConnection: nil,
			}
			request := new(uiv1.HostRequest)
			request.SetInitialize(new(uiv1.Initialization))
			initialization := uiv1.OpenRequest_builder{
				OperationId: new(initializationOperationID), Request: request, Event: nil,
				ConnectionEvent: nil, Close: nil,
			}.Build()

			// Act by initializing while the UI sends the early cancellation request.
			err := transport.initialize(t.Context(), initialization)

			// Assert the rejection is terminal and initialization still reaches ready state.
			require.NoError(t, err)
			assert.True(t, transport.ready)
		})
	}
}

// TestInitializationRejectsMismatchedCompletedPayload verifies Host-side request correlation.
func TestInitializationRejectsMismatchedCompletedPayload(t *testing.T) {
	t.Parallel()

	// Arrange accepted and running lifecycle followed by cancellation completion for initialization.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	wrong := new(uiv1.UICompleted)
	wrong.SetCancel(operationv1.CancelCompleted_builder{
		TargetState: new(operationv1.TerminalState_TERMINAL_STATE_COMPLETED),
	}.Build())
	completed := new(uiv1.UIEvent)
	completed.SetCompleted(wrong)
	stream.EXPECT().Send(gomock.Any()).Return(nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(accepted), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(completed), nil)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: false,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false, failConnection: nil,
	}
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	initialization := uiv1.OpenRequest_builder{
		OperationId: new(initializationOperationID), Request: request, Event: nil,
		ConnectionEvent: nil, Close: nil,
	}.Build()

	// Act through initialization lifecycle receipt.
	err := transport.initialize(t.Context(), initialization)

	// Assert FailedPrecondition and no readiness transition.
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "initialization")
	assert.False(t, transport.ready)
}

// TestInitializationCancellationUsesSeparateOperation verifies blocked startup cancellation correlation.
func TestInitializationCancellationUsesSeparateOperation(t *testing.T) {
	t.Parallel()

	// Arrange a canceled caller and complete target and cancellation lifecycles.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	gomock.InOrder(
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *uiv1.OpenRequest) error {
			assert.Equal(t, initializationOperationID, request.GetOperationId())
			assert.NotNil(t, request.GetRequest().GetInitialize())
			return nil
		}),
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *uiv1.OpenRequest) error {
			assert.Equal(t, initializationCancellationOperationID, request.GetOperationId())
			assert.Equal(t, initializationOperationID, request.GetRequest().GetCancel().GetTargetOperationId())
			return nil
		}),
	)
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	canceled := new(uiv1.UIEvent)
	canceled.SetCanceled(new(operationv1.Canceled))
	cancelCompleted := new(uiv1.UICompleted)
	cancelCompleted.SetCancel(operationv1.CancelCompleted_builder{
		TargetState: new(operationv1.TerminalState_TERMINAL_STATE_CANCELED),
	}.Build())
	completed := new(uiv1.UIEvent)
	completed.SetCompleted(cancelCompleted)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(accepted), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(canceled), nil)
	stream.EXPECT().Recv().Return(uiCancellationLifecycleResponse(accepted), nil)
	stream.EXPECT().Recv().Return(uiCancellationLifecycleResponse(running), nil)
	stream.EXPECT().Recv().Return(uiCancellationLifecycleResponse(completed), nil)
	_, cancelStream := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancelStream, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: false,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	initialization := uiv1.OpenRequest_builder{
		OperationId: new(initializationOperationID), Request: request, Event: nil,
		ConnectionEvent: nil, Close: nil,
	}.Build()

	// Act through canceled initialization.
	err := transport.initialize(ctx, initialization)

	// Assert separate cancellation completion and no readiness activation.
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, transport.ready)
}

// TestUnsuccessfulInitializationClosesTransportAfterTerminalDrain verifies startup closure sequence.
func TestUnsuccessfulInitializationClosesTransportAfterTerminalDrain(t *testing.T) {
	t.Parallel()

	// Arrange one failed startup followed by SDK close response and EOF.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	failed := new(uiv1.UIEvent)
	failed.SetFailed(operationv1.Failed_builder{Code: new("INTERNAL"), Message: new("startup failed")}.Build())
	closeResponse := new(uiv1.OpenResponse)
	closeResponse.SetClose(new(operationv1.CloseConnection))
	stream.EXPECT().Recv().Return(uiLifecycleResponse(accepted), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(failed), nil)
	stream.EXPECT().Recv().Return(closeResponse, nil)
	stream.EXPECT().Recv().Return(nil, io.EOF)
	closeSent := make(chan struct{}, 1)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(request *uiv1.OpenRequest) error {
		if request.GetClose() != nil {
			closeSent <- struct{}{}
		}
		return nil
	})
	stream.EXPECT().CloseSend().Return(nil)
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: false,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false,
		failConnection: nil,
	}

	// Act through public initialization failure handling.
	err := transport.Initialize(t.Context(), testInitializationFrame())

	// Assert classified startup failure and requested transport closure before return.
	require.Error(t, err)
	assert.ErrorContains(t, err, "startup failed")
	select {
	case <-closeSent:
	default:
		t.Fatal("Initialize returned before CloseConnection delivery")
	}
}

// TestInitializationFailurePreservesCategoryTextAndCause verifies Host startup error semantics.
func TestInitializationFailurePreservesCategoryTextAndCause(t *testing.T) {
	t.Parallel()

	// Arrange one classified initialization failure from the UI SDK.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	failure := new(uiv1.UIEvent)
	failure.SetFailed(operationv1.Failed_builder{
		Code: new("INTERNAL"), Message: new("open presentation: terminal unavailable"),
	}.Build())
	stream.EXPECT().Send(gomock.Any()).Return(nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(accepted), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil)
	stream.EXPECT().Recv().Return(uiLifecycleResponse(failure), nil)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: false,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false, failConnection: nil,
	}
	request := new(uiv1.HostRequest)
	request.SetInitialize(new(uiv1.Initialization))
	initialization := uiv1.OpenRequest_builder{
		OperationId: new(initializationOperationID), Request: request, Event: nil,
		ConnectionEvent: nil, Close: nil,
	}.Build()

	// Act through initialization lifecycle receipt.
	err := transport.initialize(t.Context(), initialization)

	// Assert category, complete text, Unwrap, errors.Is, and errors.As.
	var classified *operationError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, "INTERNAL", classified.Code())
	require.EqualError(t, err, "open presentation: terminal unavailable")
	cause := errors.Unwrap(err)
	require.Error(t, cause)
	assert.ErrorIs(t, err, cause)
}

// uiCancellationLifecycleResponse creates one cancellation lifecycle response.
func uiCancellationLifecycleResponse(event *uiv1.UIEvent) *uiv1.OpenResponse {
	return uiv1.OpenResponse_builder{
		OperationId: new(initializationCancellationOperationID), Request: nil, Event: event, Close: nil,
	}.Build()
}

// uiLifecycleResponse creates one UI initialization lifecycle response.
func uiLifecycleResponse(event *uiv1.UIEvent) *uiv1.OpenResponse {
	return uiv1.OpenResponse_builder{
		OperationId: new(initializationOperationID), Request: nil, Event: event, Close: nil,
	}.Build()
}
