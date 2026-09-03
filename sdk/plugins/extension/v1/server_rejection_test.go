//go:build !integration

package extensionv1

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServerEmitsExactRejectionCategoriesAndKeepsStreamOpen verifies producer rejection branches and later use.
func TestServerEmitsExactRejectionCategoriesAndKeepsStreamOpen(t *testing.T) {
	t.Parallel()

	// Arrange: define rejection branches that do not require an active operation or registration transition.
	testCases := map[string]struct {
		request   *extensionpb.OpenRequest
		id        string
		code      string
		message   string
		configure func(*server)
	}{
		"empty operation ID": {
			request: openExecuteRequest(""), id: "", code: rejectionCodeInvalidArgument,
			message: "operation identifier is required", configure: nil,
		},
		"unset request kind": {
			request: extensionpb.OpenRequest_builder{
				OperationId: new("rejected"), Request: new(extensionpb.HostRequest), Close: nil,
			}.Build(),
			id: "rejected", code: rejectionCodeInvalidArgument,
			message: "extension operation request payload is required", configure: nil,
		},
		"invalid Handle payload": {
			request: openHandleRequest("rejected", ""), id: "rejected", code: rejectionCodeInvalidArgument,
			message: "handler identifier is required", configure: nil,
		},
		"invalid Execute payload": {
			request: openExecuteRequestWith("rejected", "", []byte(`{}`)),
			id:      "rejected", code: rejectionCodeInvalidArgument,
			message: "tool name is required", configure: nil,
		},
		"non-JSON Execute arguments": {
			request: openExecuteRequestWith("rejected", "tool", []byte(`{"invalid"`)),
			id:      "rejected", code: rejectionCodeInvalidArgument,
			message: "tool arguments must contain valid JSON", configure: nil,
		},
		"unregistered handler": {
			request: openHandleRequest("rejected", "missing"), id: "rejected",
			code: rejectionCodeInvalidArgument, message: `handler "missing" is not registered`, configure: nil,
		},
		"handler payload-kind mismatch": {
			request: openHandleRequest("rejected", "observer"), id: "rejected",
			code:    rejectionCodeInvalidArgument,
			message: `handler "observer" payload does not match its registered kind`,
			configure: func(server *server) {
				server.handlers["observer"] = extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST
			},
		},
		"empty cancellation target": {
			request: openCancelRequest("rejected", ""), id: "rejected", code: rejectionCodeInvalidArgument,
			message: "cancellation target identifier is required", configure: nil,
		},
		"inactive cancellation": {
			request: openCancelRequest("rejected", "inactive"), id: "rejected",
			code: rejectionCodeTargetNotActive, message: `operation "inactive" is not active`, configure: nil,
		},
	}

	// Act: run each isolated producer branch through the SDK stream.
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testServerRejectionThenSuccessfulExecute(
				t, testCase.request, testCase.id, testCase.code, testCase.message, testCase.configure,
			)
		})
	}
	t.Run("active duplicate operation ID", testServerRejectsActiveDuplicateThenSucceeds)
	t.Run("repeated Register", testServerRejectsRepeatedRegisterThenSucceeds)
	t.Run("Handle before registration completion", testServerRejectsHandleBeforeRegistrationThenRegisters)

	// Assert: delegated checks require exact rejection data, no rejected lifecycle, and later completed work.
}

// TestServerRejectsRequestWithoutContent verifies missing stream content terminates with FailedPrecondition.
func TestServerRejectsRequestWithoutContent(t *testing.T) {
	t.Parallel()

	// Arrange: send one OpenRequest with neither request nor close content.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	secondReceiveCalled := make(chan struct{})
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	gomock.InOrder(
		stream.EXPECT().Recv().Return(extensionpb.OpenRequest_builder{
			OperationId: new("invalid"), Request: nil, Close: nil,
		}.Build(), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			close(secondReceiveCalled)
			return nil, io.EOF
		}),
	)

	// Act: process the contentless request.
	err := newServer(service).Open(stream)

	// Assert: join the receiver and terminate with the exact protocol status and complete text.
	requireSignal(t, secondReceiveCalled, "second receive was not called before server return")
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "extension stream message requires a request or close")
}

// testServerRejectionThenSuccessfulExecute verifies one rejection followed by completed Execute on the same stream.
func testServerRejectionThenSuccessfulExecute(
	t *testing.T,
	rejectedRequest *extensionpb.OpenRequest,
	rejectedID string,
	expectedCode string,
	expectedMessage string,
	configure func(*server),
) {
	t.Helper()

	controller := gomock.NewController(t)
	service := NewMockService(controller)
	execute := NewMockExecuteOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execute, nil)
	execute.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		extensionpb.ToolResult_builder{IsError: new(false), Contents: validTextContents("done")}.Build(), nil,
	)
	execute.EXPECT().Release()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	laterCompleted := make(chan struct{})
	gomock.InOrder(
		stream.EXPECT().Recv().Return(rejectedRequest, nil),
		stream.EXPECT().Recv().Return(openExecuteRequest("later"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-laterCompleted
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 4)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		if response.GetOperationId() == "later" && response.GetEvent().GetCompleted() != nil {
			close(laterCompleted)
		}
		return nil
	})
	server := newServer(service)
	server.ready = true
	if configure != nil {
		configure(server)
	}

	// Act: process the rejected request and later valid work through request EOF.
	err := server.Open(stream)

	// Assert: emit only exact Rejected data for the rejected ID and complete later work.
	require.NoError(t, err)
	assertExactRejectionWithoutLifecycle(t, responses, rejectedID, expectedCode, expectedMessage)
	assertCompletedResponse(t, responses, "later")
}

// testServerRejectsActiveDuplicateThenSucceeds verifies duplicate ownership rejection without stopping work.
func testServerRejectsActiveDuplicateThenSucceeds(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller := gomock.NewController(t)
	service := NewMockService(controller)
	blocked := NewMockExecuteOperation(controller)
	later := NewMockExecuteOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	runStarted := make(chan struct{})
	duplicateRejected := make(chan struct{})
	releaseRun := make(chan struct{})
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(blocked, nil)
	blocked.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, *ProgressReporter) (*extensionpb.ToolResult, error) {
			close(runStarted)
			<-releaseRun
			return extensionpb.ToolResult_builder{
				IsError: new(false), Contents: validTextContents("first"),
			}.Build(), nil
		},
	)
	blocked.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(later, nil)
	later.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		extensionpb.ToolResult_builder{IsError: new(false), Contents: validTextContents("later")}.Build(), nil,
	)
	later.EXPECT().Release()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	laterCompleted := make(chan struct{})
	gomock.InOrder(
		stream.EXPECT().Recv().Return(openExecuteRequest("duplicate"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-runStarted
			return openExecuteRequest("duplicate"), nil
		}),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-duplicateRejected
			close(releaseRun)
			return openExecuteRequest("later"), nil
		}),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-laterCompleted
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 7)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		switch {
		case response.GetOperationId() == "duplicate" && response.GetEvent().GetRejected() != nil:
			close(duplicateRejected)
		case response.GetOperationId() == "later" && response.GetEvent().GetCompleted() != nil:
			close(laterCompleted)
		}
		return nil
	})
	server := newServer(service)
	server.ready = true

	// Act: process active work, its duplicate, and later valid work.
	err := server.Open(stream)

	// Assert: reject only the duplicate request and preserve both accepted operations.
	require.NoError(t, err)
	assertExactRejectionCount(
		t, responses, "duplicate", rejectionCodeOperationIDInUse,
		"operation identifier is in use", 1,
	)
	assertOperationLifecycleCount(t, responses, "duplicate", 1)
	assertCompletedResponse(t, responses, "later")
}

// testServerRejectsRepeatedRegisterThenSucceeds verifies BUSY after completed registration without closing the stream.
func testServerRejectsRepeatedRegisterThenSucceeds(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller := gomock.NewController(t)
	service := NewMockService(controller)
	register := NewMockRegisterOperation(controller)
	execute := NewMockExecuteOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(register, nil)
	register.EXPECT().Run(gomock.Any()).Return(
		extensionpb.RegisterResponse_builder{Tools: nil, Handlers: nil}.Build(), nil,
	)
	register.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execute, nil)
	execute.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		extensionpb.ToolResult_builder{IsError: new(false), Contents: validTextContents("later")}.Build(), nil,
	)
	execute.EXPECT().Release()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	registerCompleted := make(chan struct{})
	laterCompleted := make(chan struct{})
	gomock.InOrder(
		stream.EXPECT().Recv().Return(openRegisterRequest("register"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-registerCompleted
			return openRegisterRequest("repeated"), nil
		}),
		stream.EXPECT().Recv().Return(openExecuteRequest("later"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-laterCompleted
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 7)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		switch {
		case response.GetOperationId() == "register" && response.GetEvent().GetCompleted() != nil:
			close(registerCompleted)
		case response.GetOperationId() == "later" && response.GetEvent().GetCompleted() != nil:
			close(laterCompleted)
		}
		return nil
	})

	// Act: complete registration, reject its repetition, and complete later work.
	err := newServer(service).Open(stream)

	// Assert: emit exact BUSY data without lifecycle and keep the stream usable.
	require.NoError(t, err)
	assertExactRejectionWithoutLifecycle(
		t, responses, "repeated", rejectionCodeBusy,
		"extension registration is already active or complete",
	)
	assertCompletedResponse(t, responses, "later")
}

// testServerRejectsHandleBeforeRegistrationThenRegisters verifies NOT_READY without stopping registration.
func testServerRejectsHandleBeforeRegistrationThenRegisters(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller := gomock.NewController(t)
	service := NewMockService(controller)
	register := NewMockRegisterOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(register, nil)
	register.EXPECT().Run(gomock.Any()).Return(
		extensionpb.RegisterResponse_builder{Tools: nil, Handlers: nil}.Build(), nil,
	)
	register.EXPECT().Release()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	registerCompleted := make(chan struct{})
	gomock.InOrder(
		stream.EXPECT().Recv().Return(openHandleRequest("rejected", "observer"), nil),
		stream.EXPECT().Recv().Return(openRegisterRequest("register"), nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-registerCompleted
			return nil, io.EOF
		}),
	)
	responses := make([]*extensionpb.OpenResponse, 0, 4)
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		responses = append(responses, response)
		if response.GetOperationId() == "register" && response.GetEvent().GetCompleted() != nil {
			close(registerCompleted)
		}
		return nil
	})

	// Act: reject Handle before readiness, then complete Register on the same stream.
	err := newServer(service).Open(stream)

	// Assert: emit exact NOT_READY without lifecycle and preserve later stream use.
	require.NoError(t, err)
	assertExactRejectionWithoutLifecycle(
		t, responses, "rejected", rejectionCodeNotReady, "extension registration is not complete",
	)
	assertCompletedResponse(t, responses, "register")
}

// assertExactRejectionWithoutLifecycle checks one rejected ID has no operation lifecycle event.
func assertExactRejectionWithoutLifecycle(
	t *testing.T,
	responses []*extensionpb.OpenResponse,
	id string,
	expectedCode string,
	expectedMessage string,
) {
	t.Helper()
	assertExactRejectionCount(t, responses, id, expectedCode, expectedMessage, 1)
	for _, response := range responses {
		if response.GetOperationId() != id {
			continue
		}
		event := response.GetEvent()
		assert.Nil(t, event.GetAccepted())
		assert.Nil(t, event.GetRunning())
		assert.Nil(t, event.GetProgress())
		assert.Nil(t, event.GetCompleted())
		assert.Nil(t, event.GetCanceled())
		assert.Nil(t, event.GetFailed())
	}
}

// assertExactRejectionCount checks the exact rejection payload and count for one identifier.
func assertExactRejectionCount(
	t *testing.T,
	responses []*extensionpb.OpenResponse,
	id string,
	expectedCode string,
	expectedMessage string,
	expectedCount int,
) {
	t.Helper()
	count := 0
	for _, response := range responses {
		if response.GetOperationId() != id || response.GetEvent().GetRejected() == nil {
			continue
		}
		count++
		assert.Equal(t, expectedCode, response.GetEvent().GetRejected().GetCode())
		assert.Equal(t, expectedMessage, response.GetEvent().GetRejected().GetMessage())
	}
	assert.Equal(t, expectedCount, count)
}

// assertOperationLifecycleCount checks one accepted-running-completed sequence count for an identifier.
func assertOperationLifecycleCount(
	t *testing.T,
	responses []*extensionpb.OpenResponse,
	id string,
	expectedCount int,
) {
	t.Helper()
	accepted := 0
	running := 0
	completed := 0
	for _, response := range responses {
		if response.GetOperationId() != id {
			continue
		}
		event := response.GetEvent()
		if event.GetAccepted() != nil {
			accepted++
		}
		if event.GetRunning() != nil {
			running++
		}
		if event.GetCompleted() != nil {
			completed++
		}
	}
	assert.Equal(t, expectedCount, accepted)
	assert.Equal(t, expectedCount, running)
	assert.Equal(t, expectedCount, completed)
}

// assertCompletedResponse checks that one identifier reached completed data.
func assertCompletedResponse(t *testing.T, responses []*extensionpb.OpenResponse, id string) {
	t.Helper()
	for _, response := range responses {
		if response.GetOperationId() == id && response.GetEvent().GetCompleted() != nil {
			return
		}
	}
	assert.Fail(t, "operation did not complete", "operation ID %q", id)
}
