//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestReceiveFailureJoinsBlockedWriter verifies every receive failure stops and joins transport delivery.
func TestReceiveFailureJoinsBlockedWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		receiveErr    error
		expectedCode  codes.Code
		expectedCause SessionCompletionCause
	}{
		{
			name:          "protocol",
			receiveErr:    status.Error(codes.InvalidArgument, "invalid request framing"),
			expectedCode:  codes.InvalidArgument,
			expectedCause: SessionCompletionProtocolFailure,
		},
		{
			name:          "transport",
			receiveErr:    status.Error(codes.Unavailable, "receive transport unavailable"),
			expectedCode:  codes.Unavailable,
			expectedCause: SessionCompletionTransportFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an Accepted send that remains blocked when Recv fails.
			controller := gomock.NewController(t)
			host := NewMockHostSession(controller)
			prepared := operation.NewMockPrepared[AgentEvent, Response](controller)
			prepared.EXPECT().Release()
			host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
			stream := NewMockOpenStream(controller)
			stream.EXPECT().Context().Return(t.Context()).AnyTimes()
			request := testRequest("blocked-writer", func(request *programmaticv1.ControllerRequest) {
				request.SetGetMessages(new(programmaticv1.GetMessages))
			})
			sendStarted := make(chan struct{})
			releaseSend := make(chan struct{})
			receiveReturned := make(chan struct{})
			gomock.InOrder(
				stream.EXPECT().Recv().Return(request, nil),
				stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
					<-sendStarted
					close(receiveReturned)
					return nil, test.receiveErr
				}),
			)
			stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*programmaticv1.OpenResponse) error {
				close(sendStarted)
				<-releaseSend
				return nil
			})
			service := New(t.Context(), host)
			result := make(chan error, 1)
			go func() { result <- service.open(stream) }()
			<-receiveReturned

			// Act by observing whether Open returns before the blocked writer stops.
			var rpcErr error
			returnedBeforeWriter := false
			select {
			case rpcErr = <-result:
				returnedBeforeWriter = true
			case <-time.After(100 * time.Millisecond):
			}
			close(releaseSend)
			if !returnedBeforeWriter {
				rpcErr = <-result
			}
			completion := <-service.Completions()

			// Assert Open joins Writer.Run and classifies the receive failure by source.
			assert.False(t, returnedBeforeWriter)
			assert.Equal(t, test.expectedCode, status.Code(rpcErr))
			assert.Equal(t, test.expectedCause, completion.Cause)
		})
	}
}

// TestBlockedOperationDoesNotBlockLaterRequest proves receipt and preparation stay asynchronous.
func TestBlockedOperationDoesNotBlockLaterRequest(t *testing.T) {
	t.Parallel()

	// Arrange one blocked operation followed by a snapshot query.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	blocked := operation.NewMockPrepared[AgentEvent, Response](controller)
	query := operation.NewMockPrepared[AgentEvent, Response](controller)
	releaseRun := make(chan struct{})
	blocked.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-releaseRun
			return operation.Completed(testResponse(ResponseUserRequestCompleted))
		},
	)
	blocked.EXPECT().Release()
	queryResult := testResponse(ResponseRunState)
	queryResult.State = mo.Some(RunStateResult{State: RunStateRunning, ActiveOperationID: mo.Some("blocked")})
	query.EXPECT().Run(gomock.Any(), gomock.Any()).Return(operation.Completed(queryResult))
	query.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command Command) (operation.Prepared[AgentEvent, Response], error) {
			if command.OperationID == "blocked" {
				return blocked, nil
			}
			return query, nil
		},
	).Times(2)
	stream := newStreamHarness(t, t.Context())
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("blocked", func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.UserRequest)
		payload.SetText("blocked")
		request.SetUserRequest(payload)
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "blocked" && response.GetEvent().HasRunning() {
			break
		}
	}
	stream.requests <- testRequest("query", func(request *programmaticv1.ControllerRequest) {
		request.SetGetRunState(new(programmaticv1.GetRunState))
	})

	// Act by reading until the later request completes while the first Run is blocked.
	seenQueryCompleted := false
	for !seenQueryCompleted {
		select {
		case response := <-stream.responses:
			seenQueryCompleted = response.GetOperationId() == "query" && response.GetEvent().HasCompleted()
		case <-time.After(time.Second):
			require.FailNow(t, "later request did not complete while prior work was blocked")
		}
	}

	// Assert clean terminal delivery after releasing work and half-closing.
	close(releaseRun)
	stream.closeSend()
	require.NoError(t, <-result)
}

// TestInvalidSessionMutationPayloadsStayBeforeAcceptance verifies invalid payloads do not reserve operation work.
func TestInvalidSessionMutationPayloadsStayBeforeAcceptance(t *testing.T) {
	t.Parallel()

	// Arrange every bounded session mutation validation case.
	tests := map[string]struct {
		setInvalid func(*programmaticv1.ControllerRequest)
	}{
		"whitespace session name": {
			setInvalid: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.SetSessionName)
				payload.SetName(" \t\n ")
				request.SetSetSessionName(payload)
			},
		},
		"whitespace custom focus": {
			setInvalid: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.NavigateSessionTree)
				payload.SetTargetEntryId("entry")
				payload.SetSummaryMode(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT)
				payload.SetCustomFocus(" \t\n ")
				request.SetNavigateSessionTree(payload)
			},
		},
		"unspecified summary mode": {
			setInvalid: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.NavigateSessionTree)
				payload.SetTargetEntryId("entry")
				payload.SetSummaryMode(programmaticv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED)
				request.SetNavigateSessionTree(payload)
			},
		},
		"unknown summary mode": {
			setInvalid: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.NavigateSessionTree)
				payload.SetTargetEntryId("entry")
				payload.SetSummaryMode(programmaticv1.SummaryMode(99))
				request.SetNavigateSessionTree(payload)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange one valid request after the invalid request on the same stream.
			controller := gomock.NewController(t)
			host := NewMockHostSession(controller)
			validPrepared := operation.NewMockPrepared[AgentEvent, Response](controller)
			validPrepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
				operation.Completed(testResponse(ResponseUserRequestCompleted)),
			).AnyTimes()
			validPrepared.EXPECT().Release().AnyTimes()
			invalidPrepared := operation.NewMockPrepared[AgentEvent, Response](controller)
			invalidPrepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
				operation.Failed[Response]("INTERNAL", errors.New("invalid payload reached preparation")),
			).AnyTimes()
			invalidPrepared.EXPECT().Release().AnyTimes()
			prepareCalls := 0
			host.EXPECT().Prepare(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, command Command) (operation.Prepared[AgentEvent, Response], error) {
					prepareCalls++
					if command.OperationID == "invalid" {
						return invalidPrepared, nil
					}
					return validPrepared, nil
				},
			).AnyTimes()
			stream := newStreamHarness(t, t.Context())
			service := New(t.Context(), host)
			result := make(chan error, 1)
			go func() { result <- service.open(stream.stream) }()
			stream.requests <- testRequest("invalid", test.setInvalid)

			// Act by receiving the invalid request result before sending later work.
			var invalidResponse *programmaticv1.OpenResponse
			select {
			case invalidResponse = <-stream.responses:
			case <-time.After(time.Second):
				require.FailNow(t, "invalid request result was not delivered")
			}

			// Assert rejection happened before acceptance and preparation.
			rejected := invalidResponse.GetEvent().HasRejected()
			assert.True(t, rejected)
			assert.False(t, invalidResponse.GetEvent().HasAccepted())
			assert.Equal(t, 0, prepareCalls)
			if !rejected {
				stream.closeSend()
				require.NoError(t, <-result)
				return
			}
			assert.Equal(t, RejectionCodeInvalidArgument, invalidResponse.GetEvent().GetRejected().GetCode())

			stream.requests <- testRequest("valid", func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.UserRequest)
				payload.SetText("request")
				request.SetUserRequest(payload)
			})
			validAccepted := false
			validCompleted := false
			for !validCompleted {
				select {
				case response := <-stream.responses:
					if response.GetOperationId() != "valid" {
						assert.Fail(t, "invalid request emitted more than one event")
						continue
					}
					validAccepted = validAccepted || response.GetEvent().HasAccepted()
					validCompleted = response.GetEvent().HasCompleted()
				case <-time.After(time.Second):
					require.FailNow(t, "following valid request did not complete")
				}
			}

			// Assert the stream admitted and completed the following valid request.
			assert.True(t, validAccepted)
			assert.Equal(t, 1, prepareCalls)
			stream.closeSend()
			require.NoError(t, <-result)
		})
	}
}

// TestMalformedDuplicateAndFailedOperationsKeepStreamOpen proves per-request failures do not close the connection.
func TestMalformedDuplicateAndFailedOperationsKeepStreamOpen(t *testing.T) {
	t.Parallel()

	// Arrange one active operation, one duplicate, one malformed request, and one failed operation.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	active := operation.NewMockPrepared[AgentEvent, Response](controller)
	failed := operation.NewMockPrepared[AgentEvent, Response](controller)
	releaseActive := make(chan struct{})
	active.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-releaseActive
			return operation.Completed(testResponse(ResponseUserRequestCompleted))
		},
	)
	active.EXPECT().Release()
	failed.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		operation.Failed[Response]("MODEL_FAILED", errors.New("model failed")),
	)
	failed.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command Command) (operation.Prepared[AgentEvent, Response], error) {
			if command.OperationID == "active" {
				return active, nil
			}
			return failed, nil
		},
	).Times(2)
	stream := newStreamHarness(t, t.Context())
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	user := func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.UserRequest)
		payload.SetText("request")
		request.SetUserRequest(payload)
	}
	stream.requests <- testRequest("active", user)
	stream.requests <- testRequest("active", user)
	malformed := new(programmaticv1.OpenRequest)
	malformed.SetOperationId("malformed")
	malformed.SetRequest(new(programmaticv1.ControllerRequest))
	stream.requests <- malformed
	stream.requests <- testRequest("failed", func(request *programmaticv1.ControllerRequest) {
		request.SetGetModels(new(programmaticv1.GetModels))
	})

	// Act by collecting the duplicate, malformed, and failed events.
	type publicError struct {
		code    string
		message string
	}
	rejections := make(map[string]publicError)
	failedError := publicError{}
	for len(rejections) < 2 || failedError.code == "" {
		select {
		case response := <-stream.responses:
			if response.GetEvent().HasRejected() {
				rejected := response.GetEvent().GetRejected()
				rejections[response.GetOperationId()] = publicError{
					code: rejected.GetCode(), message: rejected.GetMessage(),
				}
			}
			if response.GetOperationId() == "failed" && response.GetEvent().HasFailed() {
				failed := response.GetEvent().GetFailed()
				failedError = publicError{code: failed.GetCode(), message: failed.GetMessage()}
			}
		case <-time.After(time.Second):
			require.FailNow(t, "per-request lifecycle evidence was not delivered")
		}
	}

	// Assert exact rejection and closed failure mappings with complete error text, then close cleanly.
	assert.Equal(t, publicError{
		code: RejectionCodeOperationIDInUse, message: operation.ErrIdentifierInUse.Error(),
	}, rejections["active"])
	assert.Equal(t, publicError{
		code: RejectionCodeInvalidArgument, message: "programmatic request kind is required",
	}, rejections["malformed"])
	assert.Equal(t, publicError{code: FailureCodeInternal, message: "model failed"}, failedError)
	close(releaseActive)
	stream.closeSend()
	require.NoError(t, <-result)
}
