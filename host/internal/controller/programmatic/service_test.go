package programmatic

import (
	"context"
	"errors"
	"io"
	"strings"
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

	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	userRequest := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     new("user"),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new("request"),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
	stateRequest := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     new("state"),
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
			SessionEntries:    nil,
			SessionStatistics: mo.None[domainsession.Statistics](),
			CorrelationID:     "user",
			Kind:              ResponseUserRequestAccepted,
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[domainsession.Info](),
			Sessions:          nil,
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
				SessionEntries:    nil,
				SessionStatistics: mo.None[domainsession.Statistics](),
				CorrelationID:     "state",
				Kind:              ResponseRunState,
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

// TestAcceptedResponseDeliveryFailureDoesNotStartOperation verifies failed acceptance delivery prevents run start.
func (s *ServiceSuite) TestAcceptedResponseDeliveryFailureDoesNotStartOperation() {
	// Arrange an accepted operation whose response cannot reach the client.
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     new("accepted"),
		UserRequest:       programmaticv1.UserRequest_builder{Text: new("request")}.Build(),
		CreateSession:     nil, ListSessions: nil, ResumeSession: nil, SetSessionName: nil, GetSessionInfo: nil,
	}.Build()
	events := make(chan AgentEvent)
	stream.EXPECT().Recv().Return(request, nil)
	session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
		SessionEntries:    nil,
		SessionStatistics: mo.None[domainsession.Statistics](),
		CorrelationID:     "accepted", Kind: ResponseUserRequestAccepted, State: mo.None[RunStateResult](),
		Messages: nil, Models: mo.None[ModelsResult](), Selection: mo.None[model.Selection](),
		Rejection: mo.None[Rejection](), SessionInfo: mo.None[domainsession.Info](), Sessions: nil,
	}, operation, nil)
	operation.EXPECT().Events().Return(events)
	operation.EXPECT().Start().Times(0)
	stream.EXPECT().Send(gomock.Any()).Return(status.Error(codes.Unavailable, "acceptance delivery failed"))
	session.EXPECT().CancelAndWait(gomock.Any()).Return(nil)

	service := New(s.T().Context(), session)
	// Act by opening the controller stream.
	err := service.open(stream)
	// Assert transport failure ends the session before the operation starts.
	s.Require().Error(err)
	s.Equal(codes.Unavailable, status.Code(err))
	completion := <-service.Completions()
	s.Equal(SessionCompletionTransportFailure, completion.Cause)
}

// TestCorrelatedMissingCommandReachesHost verifies an empty correlated oneof is rejected by Host policy.
func (s *ServiceSuite) TestCorrelatedMissingCommandReachesHost() {
	// Arrange a correlated request with no selected command variant.
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()

	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries:     nil,
		GetSessionStats:       nil,
		CorrelationId:         new("missing"),
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
			SessionEntries:    nil,
			SessionStatistics: mo.None[domainsession.Statistics](),
			CorrelationID:     "missing",
			Kind:              ResponseRejected,
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

	// Act by opening the controller stream until client EOF.
	err := service.open(stream)
	// Assert Host rejection is delivered and the stream closes normally.
	s.Require().NoError(err)
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
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active Abort field.
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

// TestOperationProtocolInvariantIsTerminal verifies an immediate response cannot carry an asynchronous operation.
func (s *ServiceSuite) TestOperationProtocolInvariantIsTerminal() {
	// Arrange a run-state response paired with an invalid operation handle.
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     new("state"),
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
			SessionEntries:    nil,
			SessionStatistics: mo.None[domainsession.Statistics](),
			CorrelationID:     "state",
			Kind:              ResponseRunState,
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

	// Act by opening the controller stream.
	err := service.open(stream)
	// Assert the invariant violation terminates with a protocol failure.
	s.Equal(codes.Internal, status.Code(err))
	s.Equal(SessionCompletionProtocolFailure, (<-service.Completions()).Cause)
}

// TestAcceptedResponseRequiresOperation verifies acceptance requires a runnable operation handle.
func (s *ServiceSuite) TestAcceptedResponseRequiresOperation() {
	// Arrange an accepted response without an operation handle.
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     new("user"),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new("request"),
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
			SessionEntries:    nil,
			SessionStatistics: mo.None[domainsession.Statistics](),
			CorrelationID:     "user",
			Kind:              ResponseUserRequestAccepted,
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[domainsession.Info](),
			Sessions:          nil,
		}, nil, nil),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	// Act by opening the controller stream.
	err := service.open(stream)
	// Assert the missing operation handle terminates with a protocol failure.
	s.Equal(codes.Internal, status.Code(err))
	s.Equal(SessionCompletionProtocolFailure, (<-service.Completions()).Cause)
}

// TestAcceptedOperationRequiresEventStream verifies a runnable operation must expose its event stream.
func (s *ServiceSuite) TestAcceptedOperationRequiresEventStream() {
	// Arrange an accepted operation with no event stream.
	ctrl := gomock.NewController(s.T())
	session := NewMockHostSession(ctrl)
	operation := NewMockOperation(ctrl)
	stream := NewMockOpenStream(ctrl)
	stream.EXPECT().Context().Return(s.T().Context()).AnyTimes()
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries: nil,
		CorrelationId:     new("user"),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new("request"),
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
			SessionEntries:    nil,
			SessionStatistics: mo.None[domainsession.Statistics](),
			CorrelationID:     "user",
			Kind:              ResponseUserRequestAccepted,
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[domainsession.Info](),
			Sessions:          nil,
		}, operation, nil),
		operation.EXPECT().Events().Return(nil),
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
	)
	service := New(s.T().Context(), session)

	// Act by opening the controller stream.
	err := service.open(stream)
	// Assert the missing event stream terminates with a protocol failure.
	s.Equal(codes.Internal, status.Code(err))
	s.Equal(SessionCompletionProtocolFailure, (<-service.Completions()).Cause)
}

// TestTerminalCausePrecedence verifies application, client, transport, and cleanup precedence.
func (s *ServiceSuite) TestTerminalCausePrecedence() {
	transportErr := status.Error(codes.Unavailable, "transport failed")
	plainErr := errors.New("receive failed")
	cleanupErr := errors.New("cleanup failed")
	passthroughErr := status.Error(codes.Unavailable, "unique passthrough terminal failure")
	passthroughCleanupErr := errors.New("unique passthrough cleanup failure")
	controllerErr := errors.New("unique controller terminal failure")
	controllerCleanupErr := errors.New("unique controller cleanup failure")
	tests := map[string]struct {
		appCanceled    bool
		streamCanceled bool
		recvErr        error
		cleanupErr     error
		wantCode       codes.Code
		wantMessages   []string
		wantCause      SessionCompletionCause
		wantErr        error
	}{
		"application cancellation": {
			appCanceled:    true,
			recvErr:        transportErr,
			wantCode:       codes.Canceled,
			wantMessages:   nil,
			wantCause:      SessionCompletionApplicationCanceled,
			wantErr:        context.Canceled,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"stream cancellation": {
			streamCanceled: true,
			recvErr:        transportErr,
			wantCode:       codes.OK,
			wantMessages:   nil,
			wantCause:      SessionCompletionCleanClientClosure,
			appCanceled:    false,
			cleanupErr:     nil,
			wantErr:        nil,
		},
		"eof": {
			recvErr:        io.EOF,
			wantCode:       codes.OK,
			wantMessages:   nil,
			wantCause:      SessionCompletionCleanClientClosure,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
			wantErr:        nil,
		},
		"status": {
			recvErr:        transportErr,
			wantCode:       codes.Unavailable,
			wantMessages:   nil,
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        transportErr,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"plain receive error": {
			recvErr:        plainErr,
			wantCode:       codes.Internal,
			wantMessages:   []string{"Programmatic Control controller failed", "receive failed"},
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        plainErr,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"status and cleanup": {
			recvErr:        passthroughErr,
			cleanupErr:     passthroughCleanupErr,
			wantCode:       codes.Unavailable,
			wantMessages:   []string{"unique passthrough terminal failure", "unique passthrough cleanup failure"},
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        passthroughErr,
			appCanceled:    false,
			streamCanceled: false,
		},
		"controller and cleanup": {
			recvErr:        controllerErr,
			cleanupErr:     controllerCleanupErr,
			wantCode:       codes.Internal,
			wantMessages:   []string{"unique controller terminal failure", "unique controller cleanup failure"},
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        controllerErr,
			appCanceled:    false,
			streamCanceled: false,
		},
		"cleanup": {
			recvErr:        io.EOF,
			cleanupErr:     cleanupErr,
			wantCode:       codes.Internal,
			wantMessages:   []string{"clean up Programmatic Control session", "cleanup failed"},
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
			for _, message := range test.wantMessages {
				s.Contains(status.Convert(err).Message(), message)
			}
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

// TestApplicationCancellationPreservesSelectedTerminals verifies precedence joins independent selected causes.
func TestApplicationCancellationPreservesSelectedTerminals(t *testing.T) {
	t.Parallel()

	cleanupErr := errors.New("unique selected cleanup failure")
	tests := []struct {
		name     string
		cause    SessionCompletionCause
		terminal error
	}{
		{
			name: "protocol terminal", cause: SessionCompletionProtocolFailure,
			terminal: status.Error(codes.InvalidArgument, "unique selected protocol failure"),
		},
		{
			name: "transport terminal", cause: SessionCompletionTransportFailure,
			terminal: errors.New("unique selected transport failure"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a valid canceled application context and an already-selected independent terminal.
			applicationContext, cancelApplication := context.WithCancel(t.Context())
			cancelApplication()
			service := New(applicationContext, nil)
			selected := terminalResult{
				cause: test.cause, err: test.terminal, clean: false, passthrough: true,
			}
			eventTerminals := make(chan terminalResult, 1)
			receiveTerminals := make(chan terminalResult, 1)

			// Act through precedence and completion publication.
			terminal := service.applyTerminalPrecedence(
				t.Context(), selected, eventTerminals, receiveTerminals,
			)
			completion, rpcErr := service.complete(t.Context(), terminal, cleanupErr)

			// Assert RPC and completion ownership remain canceled while every independent cause survives once.
			assert.Equal(t, codes.Canceled, status.Code(rpcErr))
			assert.Equal(t, SessionCompletionApplicationCanceled, completion.Cause)
			require.ErrorIs(t, completion.Err, context.Canceled)
			require.ErrorIs(t, completion.Err, test.terminal)
			assert.Equal(t, 1, strings.Count(completion.Err.Error(), context.Canceled.Error()))
			assert.Equal(t, 1, strings.Count(completion.Err.Error(), test.terminal.Error()))
			require.ErrorIs(t, completion.CleanupErr, cleanupErr)
		})
	}
}

// TestOwnerClosurePreservesSelectedAndReadyTerminals verifies clean RPC ownership keeps local errors.
func TestOwnerClosurePreservesSelectedAndReadyTerminals(t *testing.T) {
	t.Parallel()

	protocolErr := status.Error(codes.InvalidArgument, "unique owner protocol failure")
	eventErr := errors.New("unique owner event failure")
	transportErr := status.Error(codes.Unavailable, "unique owner transport failure")
	cleanupErr := errors.New("unique owner cleanup failure")
	tests := []struct {
		name          string
		cancelStream  bool
		selected      terminalResult
		eventReady    terminalResult
		receiveReady  terminalResult
		cleanupErr    error
		expectedErr   []error
		expectedCause SessionCompletionCause
		expectedRPC   codes.Code
	}{
		{
			name: "selected protocol with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionProtocolFailure, err: protocolErr, clean: false, passthrough: true,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: cleanupErr,
			expectedErr: []error{protocolErr, cleanupErr}, expectedCause: SessionCompletionProtocolFailure,
			expectedRPC: codes.Internal,
		},
		{
			name: "selected event with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionProtocolFailure, err: eventErr, clean: false, passthrough: false,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: []error{eventErr}, expectedCause: SessionCompletionProtocolFailure,
			expectedRPC: codes.OK,
		},
		{
			name: "selected transport with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionTransportFailure, err: transportErr, clean: false, passthrough: true,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: []error{transportErr}, expectedCause: SessionCompletionTransportFailure,
			expectedRPC: codes.OK,
		},
		{
			name: "buffered event with EOF", cancelStream: false,
			selected: terminalResult{
				cause: SessionCompletionCleanClientClosure, err: io.EOF, clean: true, passthrough: false,
			},
			eventReady: terminalResult{
				cause: SessionCompletionProtocolFailure, err: eventErr, clean: false, passthrough: false,
			},
			receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: []error{eventErr}, expectedCause: SessionCompletionProtocolFailure,
			expectedRPC: codes.OK,
		},
		{
			name: "buffered receive with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
			},
			eventReady: terminalResult{},
			receiveReady: terminalResult{
				cause: SessionCompletionTransportFailure, err: transportErr, clean: false, passthrough: true,
			},
			cleanupErr: nil, expectedErr: []error{transportErr},
			expectedCause: SessionCompletionTransportFailure, expectedRPC: codes.OK,
		},
		{
			name: "pure EOF closure", cancelStream: false,
			selected: terminalResult{
				cause: SessionCompletionCleanClientClosure, err: io.EOF, clean: true, passthrough: false,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: nil, expectedCause: SessionCompletionCleanClientClosure, expectedRPC: codes.OK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange selected and buffered terminals with a valid owner stream context.
			applicationContext := t.Context()
			streamContext, cancelStream := context.WithCancel(t.Context())
			if test.cancelStream {
				cancelStream()
			} else {
				t.Cleanup(cancelStream)
			}
			service := New(applicationContext, nil)
			eventTerminals := make(chan terminalResult, 1)
			receiveTerminals := make(chan terminalResult, 1)
			if test.eventReady.err != nil {
				eventTerminals <- test.eventReady
			}
			if test.receiveReady.err != nil {
				receiveTerminals <- test.receiveReady
			}

			// Act through owner-closure precedence and completion publication.
			terminal := service.applyTerminalPrecedence(
				streamContext, test.selected, eventTerminals, receiveTerminals,
			)
			completion, rpcErr := service.complete(streamContext, terminal, test.cleanupErr)

			// Assert RPC closure behavior and every independent local completion cause.
			assert.Equal(t, test.expectedRPC, status.Code(rpcErr))
			assert.Equal(t, test.expectedCause, completion.Cause)
			if len(test.expectedErr) == 0 {
				require.NoError(t, completion.Err)
			} else {
				for _, expectedErr := range test.expectedErr {
					require.ErrorIs(t, completion.Err, expectedErr)
					assert.Equal(t, 1, strings.Count(completion.Err.Error(), expectedErr.Error()))
				}
			}
			require.NotErrorIs(t, completion.Err, io.EOF)
			require.NotErrorIs(t, completion.Err, context.Canceled)
		})
	}
}

// TestCleanReceivePreservesSelectedEventTerminal verifies half-close cannot replace selected event failure.
func TestCleanReceivePreservesSelectedEventTerminal(t *testing.T) {
	t.Parallel()

	eventErr := errors.New("unique selected event failure before clean receive")
	tests := []struct {
		name          string
		selected      terminalResult
		expectedErr   error
		expectedCause SessionCompletionCause
	}{
		{
			name: "selected event and clean receive",
			selected: terminalResult{
				cause: SessionCompletionProtocolFailure, err: eventErr, clean: false, passthrough: false,
			},
			expectedErr: eventErr, expectedCause: SessionCompletionProtocolFailure,
		},
		{
			name: "pure clean receive",
			selected: terminalResult{
				cause: SessionCompletionUnspecified, err: nil, clean: false, passthrough: false,
			},
			expectedErr: nil, expectedCause: SessionCompletionCleanClientClosure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an active stream and one already-buffered clean receive terminal.
			service := New(t.Context(), nil)
			eventTerminals := make(chan terminalResult, 1)
			receiveTerminals := make(chan terminalResult, 1)
			receiveTerminals <- terminalResult{
				cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
			}

			// Act after an event terminal was selected before the clean receive became observable.
			terminal := service.applyTerminalPrecedence(
				t.Context(), test.selected, eventTerminals, receiveTerminals,
			)
			completion, rpcErr := service.complete(t.Context(), terminal, nil)

			// Assert pure receive closure is clean while an independent selected event remains once.
			require.NoError(t, rpcErr)
			assert.Equal(t, test.expectedCause, completion.Cause)
			if test.expectedErr == nil {
				require.NoError(t, completion.Err)
			} else {
				require.ErrorIs(t, completion.Err, test.expectedErr)
				assert.Equal(t, 1, strings.Count(completion.Err.Error(), test.expectedErr.Error()))
			}
		})
	}
}

// TestApplicationCancellationCollectsReadyReceiveTerminalAfterCleanup verifies late receive publication.
func TestApplicationCancellationCollectsReadyReceiveTerminalAfterCleanup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a blocked receive and cleanup barrier around valid application cancellation.
		controller := gomock.NewController(t)
		session := NewMockHostSession(controller)
		stream := NewMockOpenStream(controller)
		applicationContext, cancelApplication := context.WithCancel(t.Context())
		streamContext, cancelStream := context.WithCancel(t.Context())
		t.Cleanup(cancelStream)
		receiveStarted := make(chan struct{})
		releaseReceive := make(chan struct{})
		cleanupStarted := make(chan struct{})
		releaseCleanup := make(chan struct{})
		terminalErr := status.Error(codes.Unavailable, "unique ready receive failure")
		cleanupErr := errors.New("unique ready cleanup failure")
		stream.EXPECT().Context().Return(streamContext).AnyTimes()
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			close(receiveStarted)
			<-releaseReceive
			return nil, terminalErr
		})
		session.EXPECT().CancelAndWait(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
			require.NoError(t, ctx.Err())
			close(cleanupStarted)
			<-releaseCleanup
			return cleanupErr
		})
		service := New(applicationContext, session)
		openDone := make(chan error, 1)
		go func() { openDone <- service.open(stream) }()
		<-receiveStarted

		// Act by canceling the application, then publishing the receive terminal before cleanup completes.
		cancelApplication()
		<-cleanupStarted
		close(releaseReceive)
		synctest.Wait()
		close(releaseCleanup)
		synctest.Wait()
		rpcErr := <-openDone
		completion := <-service.Completions()

		// Assert cancellation precedence and RPC status retain the ready transport and cleanup causes once.
		assert.Equal(t, codes.Canceled, status.Code(rpcErr))
		assert.Equal(t, SessionCompletionApplicationCanceled, completion.Cause)
		require.ErrorIs(t, completion.Err, context.Canceled)
		require.ErrorIs(t, completion.Err, terminalErr)
		assert.Equal(t, 1, strings.Count(completion.Err.Error(), context.Canceled.Error()))
		assert.Equal(t, 1, strings.Count(completion.Err.Error(), terminalErr.Error()))
		require.ErrorIs(t, completion.CleanupErr, cleanupErr)
	})
}

// TestApplicationCancellationEndsBlockedReceive verifies application shutdown does not wait for another client frame.
func TestApplicationCancellationEndsBlockedReceive(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange a receive that remains blocked until the client stream context is canceled.
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

		// Act by canceling only the application context.
		cancelApplication()
		synctest.Wait()

		// Assert controller completion does not depend on releasing the client receive.
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

	// Arrange mapping and transport failures that occur while request receive is blocked.
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
				//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
				request := programmaticv1.OpenRequest_builder{
					GetSessionEntries: nil,
					CorrelationId:     new("user"),
					UserRequest: programmaticv1.UserRequest_builder{
						Text: new("request"),
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
						SessionEntries:    nil,
						SessionStatistics: mo.None[domainsession.Statistics](),
						CorrelationID:     "user",
						Kind:              ResponseUserRequestAccepted,
						State:             mo.None[RunStateResult](),
						Messages:          nil,
						Models:            mo.None[ModelsResult](),
						Selection:         mo.None[model.Selection](),
						Rejection:         mo.None[Rejection](),
						SessionInfo:       mo.None[domainsession.Info](),
						Sessions:          nil,
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
				// Act by delivering the terminal event after receive blocks.
				openDone := make(chan error, 1)
				go func() { openDone <- service.open(stream) }()
				<-receiveBlocked
				events <- test.event
				synctest.Wait()

				// Assert event failure terminates the controller before receive is released.
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
