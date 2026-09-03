//go:build !integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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

const (
	// serverTestTimeout bounds controlled stream and operation coordination.
	serverTestTimeout = 5 * time.Second
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
	executeRejected := make(chan struct{})
	registerCompleted := make(chan struct{})
	laterCompleted := make(chan struct{})
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
			<-registerCompleted
			<-executeRejected
			return openExecuteRequest("later"), nil
		}),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-laterCompleted
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 4)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		switch {
		case response.GetOperationId() == "register" && response.GetEvent().GetCompleted() != nil:
			close(completedSendStarted)
			<-allowCompletedSend
			close(registerCompleted)
		case response.GetOperationId() == "execute" && response.GetEvent().GetRejected() != nil:
			close(executeRejected)
		case response.GetOperationId() == "later" && response.GetEvent().GetCompleted() != nil:
			close(laterCompleted)
		}
		return nil
	})

	// Act: run the SDK server through request EOF.
	err := newServer(service).Open(stream)

	// Assert: Execute is rejected as NOT_READY before registration Completed is delivered.
	require.NoError(t, err)
	assertExactRejectionWithoutLifecycle(
		t, responses, "execute", rejectionCodeNotReady, "extension registration is not complete",
	)
	assert.Equal(t, int64(1), executePreparations.Load())
	assertCompletedResponse(t, responses, "later")
}

// TestServerProcessesCancellationWhileTargetRunIsBlocked verifies receipt, joining, and all target-state mappings.
func TestServerProcessesCancellationWhileTargetRunIsBlocked(t *testing.T) {
	t.Parallel()

	// Arrange: define every terminal outcome that can win the target cancellation race.
	testCases := map[string]struct {
		outcome       func(context.Context) (*extensionpb.ToolResult, error)
		expectedState operationpb.TerminalState
	}{
		"completed": {
			outcome: func(context.Context) (*extensionpb.ToolResult, error) {
				return extensionpb.ToolResult_builder{
					IsError: new(false), Contents: validTextContents("completed"),
				}.Build(), nil
			},
			expectedState: operationpb.TerminalState_TERMINAL_STATE_COMPLETED,
		},
		"canceled": {
			outcome:       func(ctx context.Context) (*extensionpb.ToolResult, error) { return nil, ctx.Err() },
			expectedState: operationpb.TerminalState_TERMINAL_STATE_CANCELED,
		},
		"failed": {
			outcome: func(context.Context) (*extensionpb.ToolResult, error) {
				return nil, Fail(failureCodeInternal, errors.New("target failed"))
			},
			expectedState: operationpb.TerminalState_TERMINAL_STATE_FAILED,
		},
	}

	// Act: run each outcome through a blocked target and a later cancellation request.
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Assert: require exact state mapping and target-before-cancellation terminal order.
			testServerCancellationState(t, testCase.outcome, testCase.expectedState)
		})
	}
}

// testServerCancellationState runs one blocked target through cancellation and checks stream terminal order.
func testServerCancellationState(
	t *testing.T,
	outcome func(context.Context) (*extensionpb.ToolResult, error),
	expectedState operationpb.TerminalState,
) {
	t.Helper()

	// Arrange: keep one accepted Execute in Run while Recv supplies its cancellation request.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	target := NewMockExecuteOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	runStarted := make(chan struct{})
	signalRunStarted := sync.OnceFunc(func() { close(runStarted) })
	t.Cleanup(signalRunStarted)
	releaseStarted := make(chan struct{})
	allowRelease := make(chan struct{})
	openRelease := sync.OnceFunc(func() { close(allowRelease) })
	t.Cleanup(openRelease)
	cancelCompleted := make(chan struct{})
	signalCancelCompleted := sync.OnceFunc(func() { close(cancelCompleted) })
	t.Cleanup(signalCancelCompleted)
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(target, nil)
	target.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			signalRunStarted()
			if !waitForSignal(t, ctx.Done(), "target Run did not observe cancellation") {
				return nil, errors.New("target Run cancellation wait failed")
			}
			return outcome(ctx)
		},
	)
	target.EXPECT().Release().Do(func() {
		close(releaseStarted)
		waitForSignal(t, allowRelease, "target Release gate remained closed")
	})
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	gomock.InOrder(
		stream.EXPECT().Recv().Return(openExecuteRequest("target"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			if !waitForSignal(t, runStarted, "target Run did not start before cancellation receipt") {
				return nil, errors.New("target Run start wait failed")
			}
			return openCancelRequest("cancel", "target"), nil
		}),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			if !waitForSignal(t, cancelCompleted, "cancellation terminal event was not delivered") {
				return nil, errors.New("cancellation terminal wait failed")
			}
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 7)
	var cancelAcceptedBeforeRelease atomic.Bool
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		if response.GetOperationId() == "cancel" && response.GetEvent().GetAccepted() != nil {
			select {
			case <-releaseStarted:
			default:
				cancelAcceptedBeforeRelease.Store(true)
			}
		}
		if response.GetOperationId() == "cancel" && response.GetEvent().GetCompleted() != nil {
			signalCancelCompleted()
		}
		return nil
	})
	server := newServer(service)
	server.ready = true
	result := make(chan error, 1)

	// Act: run the stream until target Release blocks after observing targeted cancellation.
	go func() { result <- server.Open(stream) }()
	requireSignal(t, releaseStarted, "target release did not start after cancellation receipt")

	// Assert: cancellation admission was received before target release and completion waits for release.
	assert.True(t, cancelAcceptedBeforeRelease.Load())
	select {
	case <-cancelCompleted:
		require.FailNow(t, "cancellation completed before target release finished")
	default:
	}
	openRelease()
	require.NoError(t, requireServerResult(t, result, "server did not finish after target release"))
	assertCancellationTerminalOrder(t, responses, expectedState)
}

// assertCancellationTerminalOrder checks target state and target-before-cancellation terminal delivery.
func assertCancellationTerminalOrder(
	t *testing.T,
	responses []*extensionpb.OpenResponse,
	expectedState operationpb.TerminalState,
) {
	t.Helper()
	targetTerminalIndex := -1
	cancelTerminalIndex := -1
	for index, response := range responses {
		switch response.GetOperationId() {
		case "target":
			if extensionEventTerminalState(response.GetEvent()) == expectedState {
				targetTerminalIndex = index
			}
		case "cancel":
			completed := response.GetEvent().GetCompleted().GetCancel()
			if completed != nil {
				cancelTerminalIndex = index
				assert.Equal(t, expectedState, completed.GetTargetState())
			}
		}
	}
	assert.NotEqual(t, -1, targetTerminalIndex)
	assert.Greater(t, cancelTerminalIndex, targetTerminalIndex)
}

// extensionEventTerminalState maps one target terminal event to its shared protobuf state.
func extensionEventTerminalState(event *extensionpb.ExtensionEvent) operationpb.TerminalState {
	switch {
	case event.GetCompleted() != nil:
		return operationpb.TerminalState_TERMINAL_STATE_COMPLETED
	case event.GetCanceled() != nil:
		return operationpb.TerminalState_TERMINAL_STATE_CANCELED
	case event.GetFailed() != nil:
		return operationpb.TerminalState_TERMINAL_STATE_FAILED
	default:
		return operationpb.TerminalState_TERMINAL_STATE_UNSPECIFIED
	}
}

// TestServerClosureJoinsActiveWork verifies requested close and transport loss wait for owned cleanup.
func TestServerClosureJoinsActiveWork(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		receiveClosure func() (*extensionpb.OpenRequest, error)
		expectedCode   codes.Code
	}{
		"requested close": {
			receiveClosure: func() (*extensionpb.OpenRequest, error) {
				return extensionpb.OpenRequest_builder{
					OperationId: new(""), Request: nil, Close: new(operationpb.CloseConnection),
				}.Build(), nil
			},
			expectedCode: codes.OK,
		},
		"transport loss": {
			receiveClosure: func() (*extensionpb.OpenRequest, error) {
				return nil, status.Error(codes.Unavailable, "extension transport lost")
			},
			expectedCode: codes.Unavailable,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange: keep one accepted Execute active until connection cleanup cancels it.
			controller := gomock.NewController(t)
			service := NewMockService(controller)
			target := NewMockExecuteOperation(controller)
			stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
			runStarted := make(chan struct{})
			signalRunStarted := sync.OnceFunc(func() { close(runStarted) })
			t.Cleanup(signalRunStarted)
			releaseStarted := make(chan struct{})
			allowRelease := make(chan struct{})
			openRelease := sync.OnceFunc(func() { close(allowRelease) })
			t.Cleanup(openRelease)
			service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(target, nil)
			target.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
					signalRunStarted()
					if !waitForSignal(t, ctx.Done(), "target Run did not observe connection cleanup") {
						return nil, errors.New("target connection cleanup wait failed")
					}
					return nil, ctx.Err()
				},
			)
			target.EXPECT().Release().Do(func() {
				close(releaseStarted)
				waitForSignal(t, allowRelease, "target connection cleanup Release gate remained closed")
			})
			stream.EXPECT().Context().AnyTimes().Return(t.Context())
			receiveCalls := []any{
				stream.EXPECT().Recv().Return(openExecuteRequest("target"), nil),
				stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
					if !waitForSignal(t, runStarted, "target Run did not start before connection cleanup") {
						return nil, errors.New("target Run start wait failed")
					}
					return testCase.receiveClosure()
				}),
			}
			if testCase.expectedCode == codes.OK {
				receiveCalls = append(receiveCalls, stream.EXPECT().Recv().Return(nil, io.EOF))
			}
			gomock.InOrder(receiveCalls...)
			stream.EXPECT().Send(gomock.Any()).AnyTimes().Return(nil)
			server := newServer(service)
			server.ready = true
			result := make(chan error, 1)

			// Act: close or fail the stream after target Run starts.
			go func() { result <- server.Open(stream) }()
			requireSignal(t, releaseStarted, "target release did not start during connection cleanup")

			// Assert: Open does not return before owned Release finishes.
			select {
			case <-result:
				require.FailNow(t, "Open returned before active release finished")
			default:
			}
			openRelease()
			err := requireServerResult(t, result, "server did not finish after connection cleanup")
			assert.Equal(t, testCase.expectedCode, status.Code(err))
			if testCase.expectedCode != codes.OK {
				assert.ErrorContains(t, err, "extension transport lost")
			}
		})
	}
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

// TestValidateRejectionCodeUsesRequestSpecificClosedSets verifies every Extension request category set.
func TestValidateRejectionCodeUsesRequestSpecificClosedSets(t *testing.T) {
	t.Parallel()

	// Arrange: define every supported category and the exact allowed set for each request kind.
	allCodes := []string{
		rejectionCodeInvalidArgument,
		rejectionCodeOperationIDInUse,
		rejectionCodeBusy,
		rejectionCodeNotReady,
		rejectionCodeTargetNotActive,
		"UNSUPPORTED",
	}
	allowed := map[requestKind]map[string]bool{
		requestRegister: {
			rejectionCodeInvalidArgument: true, rejectionCodeOperationIDInUse: true,
			rejectionCodeBusy: true, rejectionCodeNotReady: true,
		},
		requestHandle: {
			rejectionCodeInvalidArgument: true, rejectionCodeOperationIDInUse: true,
			rejectionCodeBusy: true, rejectionCodeNotReady: true,
		},
		requestExecute: {
			rejectionCodeInvalidArgument: true, rejectionCodeOperationIDInUse: true,
			rejectionCodeBusy: true, rejectionCodeNotReady: true,
		},
		requestCancel: {
			rejectionCodeInvalidArgument: true, rejectionCodeOperationIDInUse: true,
			rejectionCodeTargetNotActive: true,
		},
	}

	for kind, allowedCodes := range allowed {
		for _, code := range allCodes {
			// Act: validate one category at the request-specific boundary.
			err := validateRejectionCode(kind, code)

			// Assert: accept only the documented closed-set members.
			if allowedCodes[code] {
				assert.NoError(t, err, "kind %d code %s", kind, code)
			} else {
				assert.Error(t, err, "kind %d code %s", kind, code)
			}
		}
	}
}

// TestPublicErrorWrappersPreserveCauses verifies public classified errors retain exact text and cause identity.
func TestPublicErrorWrappersPreserveCauses(t *testing.T) {
	t.Parallel()

	// Arrange: create distinct local causes for both public classified error types.
	rejectionCause := errors.New("complete rejection cause")
	failureCause := errors.New("complete failure cause")

	// Act: wrap each cause through the public SDK helpers.
	rejection := Reject(rejectionCodeBusy, rejectionCause)
	failure := Fail(failureCodeInternal, failureCause)

	// Assert: preserve exact text, category, concrete type, and original cause.
	var rejectionError *RejectionError
	require.ErrorAs(t, rejection, &rejectionError)
	assert.Equal(t, rejectionCodeBusy, rejectionError.Code())
	assert.EqualError(t, rejection, rejectionCause.Error())
	assert.ErrorIs(t, rejection, rejectionCause)
	var failureError *FailureError
	require.ErrorAs(t, failure, &failureError)
	assert.Equal(t, failureCodeInternal, failureError.Code())
	assert.EqualError(t, failure, failureCause.Error())
	assert.ErrorIs(t, failure, failureCause)
}

// TestMapExtensionEventRejectsInvalidCodesAndCancelStates verifies authoritative Host event validation.
func TestMapExtensionEventRejectsInvalidCodesAndCancelStates(t *testing.T) {
	t.Parallel()

	// Arrange: create unsupported peer codes and invalid cancellation completion states.
	testCases := map[string]struct {
		kind              requestKind
		event             *extensionpb.ExtensionEvent
		expectedFragments []string
		category          string
		message           string
	}{
		"Register rejection code": {
			kind:  requestRegister,
			event: rejectedEvent(rejectionCodeTargetNotActive, "complete Register rejection text"),
			expectedFragments: []string{
				`unsupported extension rejection code "TARGET_NOT_ACTIVE"`,
				"for request kind", "peer rejection text", "complete Register rejection text",
			},
			category: rejectionCodeTargetNotActive, message: "complete Register rejection text",
		},
		"Handle rejection code": {
			kind:  requestHandle,
			event: rejectedEvent(rejectionCodeTargetNotActive, "complete Handle rejection text"),
			expectedFragments: []string{
				`unsupported extension rejection code "TARGET_NOT_ACTIVE"`,
				"for request kind", "peer rejection text", "complete Handle rejection text",
			},
			category: rejectionCodeTargetNotActive, message: "complete Handle rejection text",
		},
		"Execute rejection code": {
			kind: requestExecute, event: rejectedEvent("UNSUPPORTED", "complete Execute rejection text"),
			expectedFragments: []string{
				`unsupported extension rejection code "UNSUPPORTED"`,
				"for request kind", "peer rejection text", "complete Execute rejection text",
			},
			category: "UNSUPPORTED", message: "complete Execute rejection text",
		},
		"Cancel rejection code": {
			kind: requestCancel, event: rejectedEvent(rejectionCodeBusy, "complete Cancel rejection text"),
			expectedFragments: []string{
				`unsupported extension rejection code "BUSY"`,
				"for request kind", "peer rejection text", "complete Cancel rejection text",
			},
			category: rejectionCodeBusy, message: "complete Cancel rejection text",
		},
		"failure code": {
			kind: requestExecute, event: failedEvent("UNSUPPORTED", "complete failure text"),
			expectedFragments: []string{
				`unsupported extension failure code "UNSUPPORTED"`,
				"peer failure text", "complete failure text",
			},
			category: "UNSUPPORTED", message: "complete failure text",
		},
		"missing cancel state": {
			kind: requestCancel, event: cancelCompletedEvent(operationpb.CancelCompleted_builder{}.Build()),
			expectedFragments: nil, category: "", message: "",
		},
		"unspecified cancel state": {
			kind: requestCancel, event: cancelCompletedEvent(operationpb.CancelCompleted_builder{
				TargetState: new(operationpb.TerminalState_TERMINAL_STATE_UNSPECIFIED),
			}.Build()),
			expectedFragments: nil, category: "", message: "",
		},
		"unspecified progress channel": {
			kind:              requestExecute,
			event:             progressEvent(extensionpb.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED),
			expectedFragments: nil, category: "", message: "",
		},
		"unknown progress channel": {
			kind: requestExecute, event: progressEvent(extensionpb.ProgressChannel(99)),
			expectedFragments: nil, category: "", message: "",
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Act: map the peer event before Tracker.Handle.
			_, _, err := mapExtensionEvent("operation", testCase.kind, testCase.event)
			if len(testCase.expectedFragments) > 0 {
				err = peerErrorPayloadContext(err, testCase.event, false)
			}

			// Assert: reject the invalid value with every local and peer context layer exactly once.
			require.Error(t, err)
			for _, fragment := range testCase.expectedFragments {
				require.ErrorContains(t, err, fragment)
			}
			if testCase.category != "" {
				assert.Equal(t, 1, strings.Count(err.Error(), testCase.category))
				assert.Equal(t, 1, strings.Count(err.Error(), testCase.message))
			}
		})
	}
}

// TestServerRejectsUnsupportedLocalCodes verifies SDK misuse fails the stream with Internal.
func TestServerRejectsUnsupportedLocalCodes(t *testing.T) {
	t.Parallel()

	// Arrange: configure unsupported preparation and execution failure codes.
	testCases := map[string]struct {
		prepare           func(*MockService, *MockExecuteOperation, chan struct{})
		expectedFragments []string
	}{
		"rejection": {
			prepare: func(service *MockService, _ *MockExecuteOperation, _ chan struct{}) {
				service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(
					nil, Reject("UNKNOWN", fmt.Errorf("unsupported rejection cause: %w", assert.AnError)),
				)
			},
			expectedFragments: []string{
				`unsupported extension rejection code "UNKNOWN"`,
				"for request kind 4",
				"unsupported rejection cause",
				assert.AnError.Error(),
			},
		},
		"failure": {
			prepare: func(service *MockService, execute *MockExecuteOperation, ran chan struct{}) {
				service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execute, nil)
				execute.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, *ProgressReporter) (*extensionpb.ToolResult, error) {
						close(ran)
						return nil, Fail("UNKNOWN", fmt.Errorf("unsupported failure cause: %w", assert.AnError))
					},
				)
				execute.EXPECT().Release()
			},
			expectedFragments: []string{
				"map extension terminal",
				`unsupported extension failure code "UNKNOWN"`,
				"unsupported failure cause",
				assert.AnError.Error(),
			},
		},
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
			require.Equal(t, codes.Internal, status.Code(err))
			for _, fragment := range testCase.expectedFragments {
				require.ErrorContains(t, err, fragment)
			}
			require.ErrorIs(t, err, assert.AnError)
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

// TestConnectionCloseJoinsTransportCleanup verifies Host shutdown waits for controlled transport cleanup.
func TestConnectionCloseJoinsTransportCleanup(t *testing.T) {
	t.Parallel()

	// Arrange: open a Host connection with one tracked operation and a controlled transport loss.
	controller := gomock.NewController(t)
	service := NewMockExtensionServiceClient(controller)
	stream := NewMockExtensionService_OpenClient[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	service.EXPECT().Open(gomock.Any()).Return(stream, nil)
	receiveEntered := make(chan struct{})
	loseTransport := make(chan struct{})
	stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenResponse, error) {
		close(receiveEntered)
		<-loseTransport
		return nil, status.Error(codes.Unavailable, "controlled Host transport loss")
	})
	requestSent := make(chan struct{})
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*extensionpb.OpenRequest) error {
		close(requestSent)
		return nil
	})
	cleanupStarted := make(chan struct{})
	allowCleanup := make(chan struct{})
	openCleanup := sync.OnceFunc(func() { close(allowCleanup) })
	t.Cleanup(openCleanup)
	stream.EXPECT().CloseSend().DoAndReturn(func() error {
		close(cleanupStarted)
		<-allowCleanup
		return nil
	})
	client := &Client{
		process: nil, service: service, done: nil, version: ProtocolVersion, closeOnce: sync.Once{},
	}
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	requireSignal(t, receiveEntered, "Host receive did not start")
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("tool"), ArgumentsJson: []byte(`{}`),
	}.Build())
	started, err := connection.Start(t.Context(), "execute", request)
	require.NoError(t, err)
	requireSignal(t, requestSent, "Host request was not sent before transport loss")

	// Act: fail transport, observe the operation error, and start Host shutdown.
	close(loseTransport)
	waitResult := make(chan error, 1)
	go func() {
		_, operationErr := started.Wait(t.Context(), nil)
		waitResult <- operationErr
	}()
	waitErr := requireServerResult(t, waitResult, "tracked operation did not observe Host transport loss")
	assert.Equal(t, codes.Unavailable, status.Code(waitErr))
	closeResult := make(chan error, 1)
	go func() { closeResult <- connection.Close() }()
	requireSignal(t, cleanupStarted, "Host transport cleanup did not start")

	// Assert: shutdown cannot return before controlled transport cleanup finishes.
	select {
	case closeErr := <-closeResult:
		require.FailNowf(t, "Host shutdown returned before transport cleanup finished", "error: %v", closeErr)
	default:
	}
	openCleanup()
	closeErr := requireServerResult(t, closeResult, "Host shutdown did not finish after transport cleanup")
	assert.Equal(t, codes.Unavailable, status.Code(closeErr))
	assert.ErrorContains(t, closeErr, "controlled Host transport loss")
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

// waitForSignal waits from any test goroutine and records a direct failure when the signal is missing.
func waitForSignal(t *testing.T, signal <-chan struct{}, failure string) bool {
	t.Helper()
	select {
	case <-signal:
		return true
	case <-time.After(serverTestTimeout):
		assert.Fail(t, failure)
		return false
	}
}

// requireSignal waits for one controlled server signal or fails with the supplied reason.
func requireSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(serverTestTimeout):
		require.FailNow(t, failure)
	}
}

// requireServerResult waits for server completion or fails with the supplied reason.
func requireServerResult(t *testing.T, result <-chan error, failure string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(serverTestTimeout):
		require.FailNow(t, failure)
		return nil
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
	return openExecuteRequestWith(id, "tool", []byte(`{}`))
}

// openExecuteRequestWith constructs an Execute stream message with selected payload fields.
func openExecuteRequestWith(id string, toolName string, arguments []byte) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new(toolName), ArgumentsJson: arguments,
	}.Build())
	return extensionpb.OpenRequest_builder{OperationId: new(id), Request: request, Close: nil}.Build()
}

// openHandleRequest constructs one session-tree Handle stream message.
func openHandleRequest(id string, handlerID string) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetHandle(extensionpb.HandleRequest_builder{
		HandlerId: new(handlerID), SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil,
		SessionTree: extensionpb.SessionTreeInvocation_builder{
			SessionId: new("session"), TargetEntryId: new("target"), PrecedingActiveLeafId: nil,
			NavigationDestinationId: nil, CommittedActiveLeafId: nil, CreatedSummary: nil,
		}.Build(),
	}.Build())
	return extensionpb.OpenRequest_builder{OperationId: new(id), Request: request, Close: nil}.Build()
}

// openCancelRequest constructs one targeted cancellation stream message.
func openCancelRequest(id string, targetID string) *extensionpb.OpenRequest {
	request := new(extensionpb.HostRequest)
	request.SetCancel(operationpb.CancelOperation_builder{TargetOperationId: new(targetID)}.Build())
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
