package programmatic

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainsession "github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

type ServiceSuite struct {
	suite.Suite
}

func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// TestAcceptedOperationSendsAcceptanceBeforeStartAndReceivesCommands verifies the duplex operation flow.
func (s *ServiceSuite) TestAcceptedOperationSendsAcceptanceBeforeStartAndReceivesCommands() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()

	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	userRequest := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     proto.String("user"),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: proto.String("request"),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
	stateRequest := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     proto.String("state"),
		GetRunState:       programmaticv1.GetRunState_builder{}.Build(),
		CreateSession:     nil,
		ListSessions:      nil,
		ResumeSession:     nil,
		SetSessionName:    nil,
		GetSessionInfo:    nil,
	}.Build()
	events := make(chan AgentEvent)
	acceptanceSent := make(chan struct{})
	started := make(chan struct{})
	eventSendEntered := make(chan struct{})
	stateHandled := make(chan struct{})
	releaseEventSend := make(chan struct{})
	operation.EXPECT().Events().Return(events)
	operation.EXPECT().Start().Do(func() {
		select {
		case <-acceptanceSent:
		default:
			s.Fail("operation started before acceptance")
		}
		close(started)
	})
	gomock.InOrder(
		stream.EXPECT().Recv().Return(userRequest, nil),
		session.EXPECT().Handle(gomock.Any(), Command{
			CorrelationID:   "user",
			Kind:            CommandUserRequest,
			UserText:        mo.Some("request"),
			ProviderID:      mo.None[model.ProviderID](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[domainsession.ID](),
			SessionName:     mo.None[string](),
		}).Return(Response{
			SessionEntries: nil,
			CorrelationID:  "user",
			Kind:           ResponseUserRequestAccepted,
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[domainsession.Info](),
			Sessions:       nil,
		}, operation, nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			<-started
			<-eventSendEntered
			return stateRequest, nil
		}),
		session.EXPECT().Handle(gomock.Any(), Command{
			CorrelationID:   "state",
			Kind:            CommandGetRunState,
			UserText:        mo.None[string](),
			ProviderID:      mo.None[model.ProviderID](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[domainsession.ID](),
			SessionName:     mo.None[string](),
		}).DoAndReturn(func(context.Context, Command) (Response, Operation, error) {
			close(stateHandled)
			return Response{
				SessionEntries: nil,
				CorrelationID:  "state",
				Kind:           ResponseRunState,
				State: mo.Some(RunStateResult{
					State:               RunStateRunning,
					ActiveCorrelationID: mo.Some("user"),
				}),
				Messages:    nil,
				Models:      mo.None[ModelsResult](),
				Selection:   mo.None[model.Selection](),
				Rejection:   mo.None[Rejection](),
				SessionInfo: mo.None[domainsession.Info](),
				Sessions:    nil,
			}, nil, nil
		}),
		stream.EXPECT().Recv().Return(nil, io.EOF),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)

	var activeSends atomic.Int32
	var concurrentSend atomic.Bool
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
		if activeSends.Add(1) != 1 {
			concurrentSend.Store(true)
		}
		defer activeSends.Add(-1)
		switch {
		case response.GetCommandResponse().HasUserRequestAccepted():
			close(acceptanceSent)
		case response.HasAgentEvent():
			close(eventSendEntered)
			<-releaseEventSend
		case response.GetCommandResponse().HasRunState():
		default:
			s.Fail("unexpected response")
		}
		return nil
	}).Times(3)

	go func() {
		<-started
		events <- AgentEvent{
			CorrelationID:   "user",
			Type:            AgentEventAgentStart,
			RunID:           "run",
			ModelContent:    mo.None[ModelContent](),
			ToolCallPreview: mo.None[ToolCallPreview](),
			FinalToolCall:   mo.None[FinalToolCall](),
			ToolExecution:   mo.None[ToolExecution](),
			ToolProgress:    mo.None[ToolProgress](),
			ToolResult:      mo.None[ToolResult](),
			ModelResponse:   mo.None[ModelResponse](),
			Turn:            mo.None[TurnSummary](),
			Agent:           mo.None[AgentSummary](),
		}
		close(events)
	}()
	go func() {
		<-stateHandled
		close(releaseEventSend)
	}()

	service := New(s.T().Context(), session)
	err := service.open(stream)
	s.Require().NoError(err)
	s.False(concurrentSend.Load())
	completion := <-service.Completions()
	s.Equal(SessionCompletionCleanClientClosure, completion.Cause)
	s.Require().NoError(completion.Err)
	s.Empty(service.Completions())
}

// TestCorrelatedMissingCommandReachesHost verifies that payload rejection remains in HostSession.
func (s *ServiceSuite) TestAcceptedResponseDeliveryFailureDoesNotStartOperation() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     proto.String("accepted"),
		UserRequest:       programmaticv1.UserRequest_builder{Text: proto.String("request")}.Build(),
		CreateSession:     nil, ListSessions: nil, ResumeSession: nil, SetSessionName: nil, GetSessionInfo: nil,
	}.Build()
	events := make(chan AgentEvent)
	stream.EXPECT().Recv().Return(request, nil)
	session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
		SessionEntries: nil,
		CorrelationID:  "accepted", Kind: ResponseUserRequestAccepted, State: mo.None[RunStateResult](),
		Messages: nil, Models: mo.None[ModelsResult](), Selection: mo.None[model.Selection](),
		Rejection: mo.None[Rejection](), SessionInfo: mo.None[domainsession.Info](), Sessions: nil,
	}, operation, nil)
	operation.EXPECT().Events().Return(events)
	operation.EXPECT().Start().Times(0)
	stream.EXPECT().Send(gomock.Any()).Return(status.Error(codes.Unavailable, "acceptance delivery failed"))
	session.EXPECT().CancelAndWait(gomock.Any()).Return(nil)

	service := New(s.T().Context(), session)
	err := service.open(stream)
	s.Require().Error(err)
	s.Equal(codes.Unavailable, status.Code(err))
	completion := <-service.Completions()
	s.Equal(SessionCompletionTransportFailure, completion.Cause)
}

func (s *ServiceSuite) TestCorrelatedMissingCommandReachesHost() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()

	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries:     nil,
		CorrelationId:         proto.String("missing"),
		UserRequest:           nil,
		Abort:                 nil,
		GetRunState:           nil,
		GetMessages:           nil,
		GetModels:             nil,
		SelectModel:           nil,
		SelectReasoningChoice: nil,
		CreateSession:         nil,
		ListSessions:          nil,
		ResumeSession:         nil,
		SetSessionName:        nil,
		GetSessionInfo:        nil,
	}.Build()
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		session.EXPECT().Handle(gomock.Any(), Command{
			CorrelationID:   "missing",
			Kind:            CommandUnspecified,
			UserText:        mo.None[string](),
			ProviderID:      mo.None[model.ProviderID](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[domainsession.ID](),
			SessionName:     mo.None[string](),
		}).Return(Response{
			SessionEntries: nil,
			CorrelationID:  "missing",
			Kind:           ResponseRejected,
			Rejection: mo.Some(Rejection{
				Command: CommandUnspecified,
				Code:    RejectionInvalidArgument,
				Message: "invalid",
			}),
			State:       mo.None[RunStateResult](),
			Messages:    nil,
			Models:      mo.None[ModelsResult](),
			Selection:   mo.None[model.Selection](),
			SessionInfo: mo.None[domainsession.Info](),
			Sessions:    nil,
		}, nil, nil),
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
			s.Equal(programmaticv1.RejectionCode_REJECTION_CODE_INVALID_ARGUMENT, response.GetCommandResponse().GetRejected().GetCode())
			return nil
		}),
		stream.EXPECT().Recv().Return(nil, io.EOF),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	s.Require().NoError(service.open(stream))
	<-service.Completions()
}

// TestSecondStreamCannotAffectOwner verifies permanent atomic owner admission.
func (s *ServiceSuite) TestSecondStreamCannotAffectOwner() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	owner := NewMockOpenStream(ctrl)
	ownerContext, cancelOwner := context.WithCancel(s.T().Context())
	owner.EXPECT().Context().Return(ownerContext).AnyTimes()
	entered := make(chan struct{})
	owner.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		close(entered)
		<-ownerContext.Done()
		return nil, ownerContext.Err()
	})
	session.EXPECT().CancelAndWait(gomock.Any()).Return(nil)
	service := New(s.T().Context(), session)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- service.open(owner) }()
	<-entered

	second := NewMockOpenStream(ctrl)
	s.Equal(codes.FailedPrecondition, status.Code(service.open(second)))
	s.Empty(service.Completions())

	cancelOwner()
	s.Require().NoError(<-ownerDone)
	s.Equal(SessionCompletionCleanClientClosure, (<-service.Completions()).Cause)
	third := NewMockOpenStream(ctrl)
	s.Equal(codes.FailedPrecondition, status.Code(service.open(third)))
	s.Empty(service.Completions())
}

// TestEmptyCorrelationIsTerminal verifies InvalidArgument bypasses HostSession and joins cleanup.
func (s *ServiceSuite) TestEmptyCorrelationIsTerminal() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active Abort field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		Abort:             programmaticv1.Abort_builder{}.Build(),
		CreateSession:     nil,
		ListSessions:      nil,
		ResumeSession:     nil,
		SetSessionName:    nil,
		GetSessionInfo:    nil,
	}.Build()
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	err := service.open(stream)
	s.Equal(codes.InvalidArgument, status.Code(err))
	completion := <-service.Completions()
	s.Equal(SessionCompletionProtocolFailure, completion.Cause)
	s.Equal(codes.InvalidArgument, status.Code(completion.Err))
}

// TestOperationProtocolInvariantIsTerminal verifies only acceptance can own an operation.
func (s *ServiceSuite) TestOperationProtocolInvariantIsTerminal() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     proto.String("state"),
		GetRunState:       programmaticv1.GetRunState_builder{}.Build(),
		CreateSession:     nil,
		ListSessions:      nil,
		ResumeSession:     nil,
		SetSessionName:    nil,
		GetSessionInfo:    nil,
	}.Build()
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
			SessionEntries: nil,
			CorrelationID:  "state",
			Kind:           ResponseRunState,
			State: mo.Some(RunStateResult{
				State:               RunStateIdle,
				ActiveCorrelationID: mo.None[string](),
			}),
			Messages:    nil,
			Models:      mo.None[ModelsResult](),
			Selection:   mo.None[model.Selection](),
			Rejection:   mo.None[Rejection](),
			SessionInfo: mo.None[domainsession.Info](),
			Sessions:    nil,
		}, operation, nil),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	err := service.open(stream)
	s.Equal(codes.Internal, status.Code(err))
	s.Equal(SessionCompletionProtocolFailure, (<-service.Completions()).Cause)
}

// TestAcceptedResponseRequiresOperation verifies acceptance always provides event ownership.
func (s *ServiceSuite) TestAcceptedResponseRequiresOperation() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     proto.String("user"),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: proto.String("request"),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
			SessionEntries: nil,
			CorrelationID:  "user",
			Kind:           ResponseUserRequestAccepted,
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[domainsession.Info](),
			Sessions:       nil,
		}, nil, nil),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	err := service.open(stream)
	s.Equal(codes.Internal, status.Code(err))
	s.Equal(SessionCompletionProtocolFailure, (<-service.Completions()).Cause)
}

// TestAcceptedOperationRequiresEventStream verifies malformed operations are not acknowledged or started.
func (s *ServiceSuite) TestAcceptedOperationRequiresEventStream() {
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     proto.String("user"),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: proto.String("request"),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
			SessionEntries: nil,
			CorrelationID:  "user",
			Kind:           ResponseUserRequestAccepted,
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[domainsession.Info](),
			Sessions:       nil,
		}, operation, nil),
		operation.EXPECT().Events().Return(nil),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	err := service.open(stream)
	s.Equal(codes.Internal, status.Code(err))
	s.Equal(SessionCompletionProtocolFailure, (<-service.Completions()).Cause)
}

// TestTerminalCausePrecedence verifies application, client, transport, and cleanup precedence.
func (s *ServiceSuite) TestTerminalCausePrecedence() {
	transportErr := status.Error(codes.Unavailable, "transport failed")
	plainErr := errors.New("receive failed")
	cleanupErr := errors.New("cleanup failed")
	tests := map[string]struct {
		appCanceled    bool
		streamCanceled bool
		recvErr        error
		cleanupErr     error
		wantCode       codes.Code
		wantCause      SessionCompletionCause
		wantErr        error
	}{
		"application cancellation": {
			appCanceled:    true,
			recvErr:        transportErr,
			wantCode:       codes.Canceled,
			wantCause:      SessionCompletionApplicationCanceled,
			wantErr:        context.Canceled,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"stream cancellation": {
			streamCanceled: true,
			recvErr:        transportErr,
			wantCode:       codes.OK,
			wantCause:      SessionCompletionCleanClientClosure,
			appCanceled:    false,
			cleanupErr:     nil,
			wantErr:        nil,
		},
		"eof": {
			recvErr:        io.EOF,
			wantCode:       codes.OK,
			wantCause:      SessionCompletionCleanClientClosure,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
			wantErr:        nil,
		},
		"status": {
			recvErr:        transportErr,
			wantCode:       codes.Unavailable,
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        transportErr,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"plain receive error": {
			recvErr:        plainErr,
			wantCode:       codes.Internal,
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        plainErr,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"cleanup": {
			recvErr:        io.EOF,
			cleanupErr:     cleanupErr,
			wantCode:       codes.Internal,
			wantCause:      SessionCompletionCleanupFailure,
			wantErr:        cleanupErr,
			appCanceled:    false,
			streamCanceled: false,
		},
	}
	for name, test := range tests {
		s.Run(name, func() {
			ctrl := gomock.NewController(s.T())
			session := NewMockHostSession(ctrl)
			stream := NewMockOpenStream(ctrl)
			appContext, cancelApp := context.WithCancel(s.T().Context())
			streamContext, cancelStream := context.WithCancel(s.T().Context())
			stream.EXPECT().Context().Return(streamContext).AnyTimes()
			if test.appCanceled {
				cancelApp()
			}
			if test.streamCanceled {
				cancelStream()
			}
			if !test.appCanceled && !test.streamCanceled {
				stream.EXPECT().Recv().Return(nil, test.recvErr)
			}
			session.EXPECT().CancelAndWait(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
				s.NoError(ctx.Err())
				return test.cleanupErr
			})
			service := New(appContext, session)

			err := service.open(stream)
			s.Equal(test.wantCode, status.Code(err))
			completion := <-service.Completions()
			s.Equal(test.wantCause, completion.Cause)
			if test.wantErr == nil {
				s.Require().NoError(completion.Err)
			} else {
				s.Require().ErrorIs(completion.Err, test.wantErr)
			}
			s.Require().ErrorIs(completion.CleanupErr, test.cleanupErr)
			cancelApp()
			cancelStream()
		})
	}
}

// TestApplicationCancellationEndsBlockedReceive verifies application shutdown does not wait for another client frame.
func TestApplicationCancellationEndsBlockedReceive(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		session := NewMockHostSession(ctrl)
		stream := NewMockOpenStream(ctrl)
		applicationContext, cancelApplication := context.WithCancel(t.Context())
		streamContext, cancelStream := context.WithCancel(t.Context())
		stream.EXPECT().Context().Return(streamContext).AnyTimes()
		receiveBlocked := make(chan struct{})
		receiveReleased := make(chan struct{})
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			close(receiveBlocked)
			<-streamContext.Done()
			close(receiveReleased)
			return nil, context.Canceled
		})
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil)
		service := New(applicationContext, session)
		openDone := make(chan error, 1)
		go func() { openDone <- service.open(stream) }()
		<-receiveBlocked

		cancelApplication()
		synctest.Wait()
		select {
		case err := <-openDone:
			assert.Equal(t, codes.Canceled, status.Code(err))
		default:
			cancelStream()
			synctest.Wait()
			require.Fail(t, "controller remained blocked in Recv after application cancellation")
		}
		completion := <-service.Completions()
		assert.Equal(t, SessionCompletionApplicationCanceled, completion.Cause)
		select {
		case <-receiveReleased:
			require.Fail(t, "receive was released before completion")
		default:
		}

		cancelStream()
		synctest.Wait()
		select {
		case <-receiveReleased:
		default:
			require.Fail(t, "receive worker did not exit after stream cancellation")
		}
	})
}

// TestEventFailureEndsBlockedReceive verifies terminal event work does not wait for another client frame.
func TestEventFailureEndsBlockedReceive(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		event        AgentEvent
		sendErr      error
		wantCode     codes.Code
		wantCause    SessionCompletionCause
		wantSendCall bool
	}{
		"mapping": {
			event: AgentEvent{
				Type: AgentEventMessageEnd,
				ModelResponse: mo.Some(ModelResponse{
					Outcome:       mo.Some(ModelOutcomeUnspecified),
					Text:          "",
					ErrorMessage:  mo.None[string](),
					Provider:      mo.None[string](),
					Model:         mo.None[string](),
					ResponseModel: mo.None[string](),
					ResponseID:    mo.None[string](),
					Usage:         mo.None[ModelUsage](),
					Diagnostics:   nil,
					Content:       nil,
				}),
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
			wantCode:     codes.Internal,
			wantCause:    SessionCompletionProtocolFailure,
			sendErr:      nil,
			wantSendCall: false,
		},
		"status send": {
			event: AgentEvent{
				CorrelationID:   "user",
				Type:            AgentEventAgentStart,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
			sendErr:      status.Error(codes.ResourceExhausted, "send failed"),
			wantCode:     codes.ResourceExhausted,
			wantCause:    SessionCompletionTransportFailure,
			wantSendCall: true,
		},
		"plain send": {
			event: AgentEvent{
				CorrelationID:   "user",
				Type:            AgentEventAgentStart,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
			sendErr:      errors.New("send failed"),
			wantCode:     codes.Internal,
			wantCause:    SessionCompletionTransportFailure,
			wantSendCall: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				session := NewMockHostSession(ctrl)
				operation := NewMockOperation(ctrl)
				stream := NewMockOpenStream(ctrl)
				streamContext, cancelStream := context.WithCancel(t.Context())
				stream.EXPECT().Context().Return(streamContext).AnyTimes()
				//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
				request := programmaticv1.OpenRequest_builder{
					GetSessionEntries: nil,
					CorrelationId:     proto.String("user"),
					UserRequest: programmaticv1.UserRequest_builder{
						Text: proto.String("request"),
					}.Build(),
					CreateSession:  nil,
					ListSessions:   nil,
					ResumeSession:  nil,
					SetSessionName: nil,
					GetSessionInfo: nil,
				}.Build()
				events := make(chan AgentEvent)
				receiveBlocked := make(chan struct{})
				receiveReleased := make(chan struct{})
				operation.EXPECT().Events().Return(events)
				operation.EXPECT().Start()
				gomock.InOrder(
					stream.EXPECT().Recv().Return(request, nil),
					session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
						SessionEntries: nil,
						CorrelationID:  "user",
						Kind:           ResponseUserRequestAccepted,
						State:          mo.None[RunStateResult](),
						Messages:       nil,
						Models:         mo.None[ModelsResult](),
						Selection:      mo.None[model.Selection](),
						Rejection:      mo.None[Rejection](),
						SessionInfo:    mo.None[domainsession.Info](),
						Sessions:       nil,
					}, operation, nil),
					stream.EXPECT().Send(gomock.Any()).Return(nil),
					stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
						close(receiveBlocked)
						<-streamContext.Done()
						close(receiveReleased)
						return nil, context.Canceled
					}),
					session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
				)
				if test.wantSendCall {
					stream.EXPECT().Send(gomock.Any()).Return(test.sendErr)
				}
				service := New(t.Context(), session)
				openDone := make(chan error, 1)
				go func() { openDone <- service.open(stream) }()
				<-receiveBlocked
				events <- test.event
				synctest.Wait()

				select {
				case err := <-openDone:
					assert.Equal(t, test.wantCode, status.Code(err))
				default:
					cancelStream()
					synctest.Wait()
					require.Fail(t, "controller remained blocked in Recv after an event terminal")
				}
				completion := <-service.Completions()
				assert.Equal(t, test.wantCause, completion.Cause)
				select {
				case <-receiveReleased:
					require.Fail(t, "receive was released before completion")
				default:
				}

				cancelStream()
				synctest.Wait()
				select {
				case <-receiveReleased:
				default:
					require.Fail(t, "receive worker did not exit after stream cancellation")
				}
			})
		})
	}
}
