//go:build !integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServerKeepsRegistrationUnreadyUntilCompletedDelivery verifies pipelined work is rejected before startup delivery.
func TestServerKeepsRegistrationUnreadyUntilCompletedDelivery(t *testing.T) {
	t.Parallel()

	// Arrange: block Register Completed transport while the stream receives Execute.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	register := NewMockRegisterOperation(controller)
	execute := NewMockExecuteOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	completedSendStarted := make(chan struct{})
	allowCompletedSend := make(chan struct{})
	var closeCompleted sync.Once
	registration := extensionpb.RegisterResponse_builder{Tools: nil, Handlers: nil}.Build()
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(register, nil)
	register.EXPECT().Run(gomock.Any()).Return(registration, nil)
	register.EXPECT().Release()
	var executePreparations atomic.Int64
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(context.Context, *extensionpb.ExecuteRequest) (ExecuteOperation, error) {
			executePreparations.Add(1)
			return execute, nil
		},
	)
	execute.EXPECT().Run(gomock.Any(), gomock.Any()).AnyTimes().Return(
		extensionpb.ToolResult_builder{IsError: new(false), Contents: validTextContents("unexpected")}.Build(), nil,
	)
	execute.EXPECT().Release().AnyTimes()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	gomock.InOrder(
		stream.EXPECT().Recv().Return(openRegisterRequest("register"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-completedSendStarted
			return openExecuteRequest("execute"), nil
		}),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			time.Sleep(50 * time.Millisecond)
			closeCompleted.Do(func() { close(allowCompletedSend) })
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 4)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		if response.GetOperationId() == "register" && response.GetEvent().GetCompleted() != nil {
			close(completedSendStarted)
			<-allowCompletedSend
		}
		return nil
	})

	// Act: run the SDK server through request EOF.
	err := newServer(service).Open(stream)

	// Assert: Execute is rejected as NOT_READY before registration Completed is delivered.
	require.NoError(t, err)
	var rejection *operationpb.Rejected
	for _, response := range responses {
		if response.GetOperationId() == "execute" {
			rejection = response.GetEvent().GetRejected()
		}
	}
	require.NotNil(t, rejection)
	assert.Equal(t, rejectionCodeNotReady, rejection.GetCode())
	assert.Zero(t, executePreparations.Load())
}

// TestServerRejectsRequestAfterClose verifies CloseConnection stops admission but not stream receipt.
func TestServerRejectsRequestAfterClose(t *testing.T) {
	t.Parallel()

	// Arrange: send CloseConnection, then an ordinary request, then EOF.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	gomock.InOrder(
		stream.EXPECT().Recv().Return(extensionpb.OpenRequest_builder{
			OperationId: new(""), Request: nil, Close: new(operationpb.CloseConnection),
		}.Build(), nil),
		stream.EXPECT().Recv().Return(openExecuteRequest("late"), nil),
		stream.EXPECT().Recv().Return(nil, io.EOF),
	)

	// Act: run the server against the invalid post-close request.
	err := newServer(service).Open(stream)

	// Assert: fail the stream with FailedPrecondition.
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, "after close")
}

// TestOperationOutcomePreservesClassifiedCancellation verifies explicit failure wins over its wrapped cause.
func TestOperationOutcomePreservesClassifiedCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: classify an error whose complete cause wraps cancellation.
	cause := fmt.Errorf("cleanup failed: %w", context.Canceled)

	// Act: map the public operation error.
	outcome := operationOutcome[struct{}](Fail(failureCodeInternal, cause))

	// Assert: retain Failed, INTERNAL, complete text, and the cancellation cause.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.Equal(t, failureCodeInternal, outcome.Code())
	assert.ErrorIs(t, outcome.Err(), context.Canceled)
	assert.ErrorContains(t, outcome.Err(), "cleanup failed")
}

// TestMapExtensionEventRejectsInvalidCodesAndCancelStates verifies authoritative Host event validation.
func TestMapExtensionEventRejectsInvalidCodesAndCancelStates(t *testing.T) {
	t.Parallel()

	// Arrange: create unsupported peer codes and invalid cancellation completion states.
	testCases := map[string]struct {
		kind  requestKind
		event *extensionpb.ExtensionEvent
	}{
		"rejection code": {kind: requestExecute, event: rejectedEvent("UNKNOWN", "bad rejection")},
		"failure code":   {kind: requestExecute, event: failedEvent("UNKNOWN", "bad failure")},
		"missing cancel state": {kind: requestCancel, event: cancelCompletedEvent(
			operationpb.CancelCompleted_builder{}.Build(),
		)},
		"unspecified cancel state": {kind: requestCancel, event: cancelCompletedEvent(
			operationpb.CancelCompleted_builder{
				TargetState: new(operationpb.TerminalState_TERMINAL_STATE_UNSPECIFIED),
			}.Build(),
		)},
		"unspecified progress channel": {kind: requestExecute, event: progressEvent(
			extensionpb.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED,
		)},
		"unknown progress channel": {kind: requestExecute, event: progressEvent(
			extensionpb.ProgressChannel(99),
		)},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Act: map the peer event before Tracker.Handle.
			_, _, err := mapExtensionEvent("operation", testCase.kind, testCase.event)

			// Assert: reject the invalid closed-set value.
			require.Error(t, err)
		})
	}
}

// TestServerRejectsUnsupportedLocalCodes verifies SDK misuse fails the stream with Internal.
func TestServerRejectsUnsupportedLocalCodes(t *testing.T) {
	t.Parallel()

	// Arrange: configure unsupported preparation and execution failure codes.
	testCases := map[string]struct {
		prepare func(*MockService, *MockExecuteOperation, chan struct{})
	}{
		"rejection": {prepare: func(service *MockService, _ *MockExecuteOperation, _ chan struct{}) {
			service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(
				nil, Reject("UNKNOWN", errors.New("unsupported rejection cause")),
			)
		}},
		"failure": {prepare: func(service *MockService, execute *MockExecuteOperation, ran chan struct{}) {
			service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execute, nil)
			execute.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, *ProgressReporter) (*extensionpb.ToolResult, error) {
					close(ran)
					return nil, Fail("UNKNOWN", errors.New("unsupported failure cause"))
				},
			)
			execute.EXPECT().Release()
		}},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			service := NewMockService(controller)
			execute := NewMockExecuteOperation(controller)
			stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
			ran := make(chan struct{})
			testCase.prepare(service, execute, ran)
			server := newServer(service)
			server.ready = true
			stream.EXPECT().Context().AnyTimes().Return(t.Context())
			stream.EXPECT().Recv().Return(openExecuteRequest("execute"), nil)
			stream.EXPECT().Recv().AnyTimes().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
				if name == "failure" {
					<-ran
				}
				return nil, io.EOF
			})
			stream.EXPECT().Send(gomock.Any()).AnyTimes().Return(nil)

			// Act: run the request with an unsupported local category.
			err := server.Open(stream)

			// Assert: fail the stream as Internal and preserve complete cause text.
			assert.Equal(t, codes.Internal, status.Code(err))
			assert.ErrorContains(t, err, "unsupported")
		})
	}
}

// TestServerStopsOnWriterSendFailure verifies writer failure cancels the connection while receive remains open.
func TestServerStopsOnWriterSendFailure(t *testing.T) {
	t.Parallel()

	// Arrange: fail Accepted delivery while the next receive waits for that failure.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	register := NewMockRegisterOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	receiveEntered := make(chan struct{})
	allowReceiveReturn := make(chan struct{})
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(register, nil)
	register.EXPECT().Release()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	stream.EXPECT().Recv().Return(openRegisterRequest("register"), nil)
	stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
		close(receiveEntered)
		<-allowReceiveReturn
		return nil, errors.New("receive remained open")
	})
	sendErr := status.Error(codes.Unavailable, "accepted send failed")
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*extensionpb.OpenResponse) error {
		<-receiveEntered
		return sendErr
	})

	// Act: run until the writer send fails.
	err := newServer(service).Open(stream)
	close(allowReceiveReturn)

	// Assert: preserve the send status and stop before operation Run.
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.ErrorContains(t, err, "accepted send failed")
}

// TestConnectionStopsOnWriterSendFailure verifies Host writer failure cancels tracked operations immediately.
func TestConnectionStopsOnWriterSendFailure(t *testing.T) {
	t.Parallel()

	// Arrange: open a mocked generated stream whose first request send fails.
	controller := gomock.NewController(t)
	service := NewMockExtensionServiceClient(controller)
	stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	service.EXPECT().Open(gomock.Any()).Return(stream, nil)
	sendErr := status.Error(codes.Unavailable, "Host request send failed")
	receiveEntered := make(chan struct{})
	allowReceiveReturn := make(chan struct{})
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*extensionpb.OpenRequest) error {
		<-receiveEntered
		return sendErr
	})
	stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
		close(receiveEntered)
		<-allowReceiveReturn
		return nil, status.Error(codes.Canceled, "stream canceled after send failure")
	})
	client := &Client{
		process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{},
	}
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	<-receiveEntered
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("tool"), ArgumentsJson: []byte(`{}`),
	}.Build())

	// Act: start the request and wait for writer failure propagation.
	started, err := connection.Start(t.Context(), "execute", request)
	require.NoError(t, err)
	_, err = started.Wait(t.Context(), nil)
	close(allowReceiveReturn)

	// Assert: preserve the writer status and complete send error text.
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.ErrorContains(t, err, "Host request send failed")
}

// TestConnectionFailPreservesValidationCause verifies Host payload rejection fails and joins the connection.
func TestConnectionFailPreservesValidationCause(t *testing.T) {
	t.Parallel()

	// Arrange: open a connection whose receive remains active until failure begins.
	controller := gomock.NewController(t)
	service := NewMockExtensionServiceClient(controller)
	stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	service.EXPECT().Open(gomock.Any()).Return(stream, nil)
	receiveEntered := make(chan struct{})
	allowReceiveReturn := make(chan struct{})
	stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
		close(receiveEntered)
		<-allowReceiveReturn
		return nil, status.Error(codes.Canceled, "failed connection canceled receive")
	})
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		close(allowReceiveReturn)
		return nil
	})
	client := &Client{
		process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{},
	}
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	<-receiveEntered
	validationCause := errors.New("result contents are empty")
	failureResult := make(chan error, 1)

	// Act: fail the connection and let CloseSend release receive cleanup.
	go func() { failureResult <- connection.Fail(validationCause) }()
	err = <-failureResult

	// Assert: expose FailedPrecondition and preserve the exact validation cause.
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.ErrorContains(t, err, validationCause.Error())
	assert.ErrorIs(t, err, validationCause)
}

// TestExtensionErrorMappingPreservesStatusTextAndCause verifies local error classification keeps Go causes.
func TestExtensionErrorMappingPreservesStatusTextAndCause(t *testing.T) {
	t.Parallel()

	// Arrange: create plain transport and queue overflow causes.
	transportCause := errors.New("socket write failed")
	queueCause := fmt.Errorf("enqueue response: %w", operation.ErrQueueFull)
	testCases := map[string]struct {
		cause error
		code  codes.Code
	}{
		"transport": {cause: transportCause, code: codes.Unavailable},
		"queue":     {cause: queueCause, code: codes.ResourceExhausted},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Act: map the local delivery failure.
			mapped := mapStreamError(testCase.cause)

			// Assert: preserve status, complete text, and original cause.
			assert.Equal(t, testCase.code, status.Code(mapped))
			assert.ErrorContains(t, mapped, testCase.cause.Error())
			assert.ErrorIs(t, mapped, testCase.cause)
		})
	}
}

// TestServerMapsPlainReceiveErrors verifies transport and decode receive failures have stable statuses.
func TestServerMapsPlainReceiveErrors(t *testing.T) {
	t.Parallel()

	// Arrange: configure one plain transport error and one protobuf decode error.
	testCases := map[string]struct {
		receiveErr error
		code       codes.Code
	}{
		"transport": {receiveErr: errors.New("socket failed"), code: codes.Unavailable},
		"decode":    {receiveErr: errors.New("proto: invalid wire-format data"), code: codes.InvalidArgument},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			service := NewMockService(controller)
			stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
			stream.EXPECT().Context().AnyTimes().Return(t.Context())
			stream.EXPECT().Recv().Return(nil, testCase.receiveErr)

			// Act: receive the plain stream failure.
			err := newServer(service).Open(stream)

			// Assert: map status and preserve complete text.
			assert.Equal(t, testCase.code, status.Code(err))
			assert.ErrorContains(t, err, testCase.receiveErr.Error())
		})
	}
}

// openRegisterRequest constructs one Register stream message.
func openRegisterRequest(id string) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetRegister(new(extensionpb.RegisterRequest))
	return extensionpb.OpenRequest_builder{OperationId: new(id), Request: request, Close: nil}.Build()
}

// openExecuteRequest constructs one Execute stream message.
func openExecuteRequest(id string) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{ToolName: new("tool"), ArgumentsJson: []byte(`{}`)}.Build())
	return extensionpb.OpenRequest_builder{OperationId: new(id), Request: request, Close: nil}.Build()
}

// validTextContents constructs one nonempty tool result content list.
func validTextContents(text string) []*extensionpb.ToolResultContent {
	//nolint:exhaustruct_v5 // The content builder sets only text.
	return []*extensionpb.ToolResultContent{extensionpb.ToolResultContent_builder{Text: new(text)}.Build()}
}

// rejectedEvent constructs one Rejected event.
func rejectedEvent(code string, message string) *extensionpb.ExtensionEvent {
	event := new(extensionpb.ExtensionEvent)
	event.SetRejected(operationpb.Rejected_builder{Code: new(code), Message: new(message)}.Build())
	return event
}

// failedEvent constructs one Failed event.
func failedEvent(code string, message string) *extensionpb.ExtensionEvent {
	event := new(extensionpb.ExtensionEvent)
	event.SetFailed(operationpb.Failed_builder{Code: new(code), Message: new(message)}.Build())
	return event
}

// progressEvent constructs one tool progress event with the selected channel.
func progressEvent(channel extensionpb.ProgressChannel) *extensionpb.ExtensionEvent {
	progress := new(extensionpb.ExtensionProgress)
	progress.SetTool(extensionpb.ToolProgress_builder{Channel: new(channel), Content: new("progress")}.Build())
	event := new(extensionpb.ExtensionEvent)
	event.SetProgress(progress)
	return event
}

// cancelCompletedEvent constructs one cancellation completion event.
func cancelCompletedEvent(completed *operationpb.CancelCompleted) *extensionpb.ExtensionEvent {
	payload := new(extensionpb.ExtensionCompleted)
	payload.SetCancel(completed)
	event := new(extensionpb.ExtensionEvent)
	event.SetCompleted(payload)
	return event
}
