package programmatic

import (
	"context"

	"io"

	"sync/atomic"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainsession "github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

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
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
		GetSessionInfo:    nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
			SessionName:     mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: SummaryModeNoSummary, CustomFocus: mo.None[string](),
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
			Sessions:          nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
			SessionName:     mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: SummaryModeNoSummary, CustomFocus: mo.None[string](),
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
				Sessions:    nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
		CreateSession:     nil, ListSessions: nil, ResumeSession: nil, SetSessionName: nil, GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
	events := make(chan AgentEvent)
	stream.EXPECT().Recv().Return(request, nil)
	session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
		SessionEntries:    nil,
		SessionStatistics: mo.None[domainsession.Statistics](),
		CorrelationID:     "accepted", Kind: ResponseUserRequestAccepted, State: mo.None[RunStateResult](),
		Messages: nil, Models: mo.None[ModelsResult](), Selection: mo.None[model.Selection](),
		Rejection: mo.None[Rejection](), SessionInfo: mo.None[domainsession.Info](), Sessions: nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
		GetSessionInfo:        nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
			SessionName:     mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: SummaryModeNoSummary, CustomFocus: mo.None[string](),
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
			Sessions:    nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
		GetSessionInfo:    nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
		GetSessionInfo:    nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
			Sessions:    nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
			Sessions:          nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
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
			Sessions:          nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
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
