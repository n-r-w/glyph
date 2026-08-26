package programmatic

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type ServiceSuite struct {
	suite.Suite
}

type selectionError struct {
	code SelectionCode
}

func (e selectionError) Error() string {
	return "safe selection failure: " + string(e.code)
}

func (e selectionError) SelectionCode() string {
	return string(e.code)
}

func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

func TestRunPreparationBusyUsesDomainError(t *testing.T) {
	t.Parallel()

	controllerMock := gomock.NewController(t)
	coordinator := NewMockCoordinator(controllerMock)
	coordinator.EXPECT().PrepareRun().Return("", session.ErrBusy)
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())

	response, operation, err := service.Handle(t.Context(), controller.Command{
		CorrelationID: "busy", Kind: controller.CommandUserRequest, UserText: mo.Some("request"),
		ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[session.ID](), SessionName: mo.None[string](),
	})
	require.NoError(t, err)
	assert.Nil(t, operation)
	assert.Equal(t, controller.RejectionBusy, response.Rejection.MustGet().Code)
}

func TestSessionReplacementPreservesNondefaultModelSelection(t *testing.T) {
	t.Parallel()

	commandWithoutArguments := func(correlationID string, kind controller.CommandKind) controller.Command {
		return controller.Command{
			CorrelationID: correlationID, Kind: kind, UserText: mo.None[string](),
			ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.None[session.ID](),
			SessionName: mo.None[string](),
		}
	}
	for _, test := range []struct {
		name string
		kind controller.CommandKind
	}{
		{name: "create", kind: controller.CommandCreateSession},
		{name: "resume", kind: controller.CommandResumeSession},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mockController := gomock.NewController(t)
			coordinator := NewMockCoordinator(mockController)
			catalog := NewMockModelCatalog(mockController)
			sessions := NewMockSessionControl(mockController)
			selection := model.Selection{
				Provider: "secondary-provider", Model: "secondary-model", ReasoningChoice: model.ReasoningChoiceHigh,
			}
			catalog.EXPECT().Models().Return(nil).Times(2)
			catalog.EXPECT().Selection().Return(selection).Times(2)
			info := session.Info{
				ID: "session-id", Name: mo.Some("session"), WorkingDirectory: "/project",
				StoragePath: mo.Some("/sessions/session.jsonl"), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
			}
			if test.kind == controller.CommandCreateSession {
				sessions.EXPECT().Create(gomock.Any()).Return(info, nil)
			} else {
				sessions.EXPECT().Resume(gomock.Any(), session.ID("session-id")).Return(info, nil)
			}
			service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, sessions, NewDelivery())

			before, _, err := service.Handle(t.Context(), commandWithoutArguments("before", controller.CommandGetModels))
			require.NoError(t, err)
			command := commandWithoutArguments("replace", test.kind)
			if test.kind == controller.CommandResumeSession {
				command.SessionID = mo.Some(session.ID("session-id"))
			}
			_, _, err = service.Handle(t.Context(), command)
			require.NoError(t, err)
			after, _, err := service.Handle(t.Context(), commandWithoutArguments("after", controller.CommandGetModels))
			require.NoError(t, err)
			require.Equal(t, mo.Some(selection), before.Models.MustGet().ActiveSelection)
			require.Equal(t, before.Models.MustGet().ActiveSelection, after.Models.MustGet().ActiveSelection)
		})
	}
}

func TestSessionErrorsUseExistingRejectionCodes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		kind         controller.CommandKind
		sessionID    mo.Option[session.ID]
		sessionName  mo.Option[string]
		operationErr error
		expected     controller.RejectionCode
	}{
		{name: "busy", kind: controller.CommandCreateSession, sessionID: mo.None[session.ID](), sessionName: mo.None[string](), operationErr: session.ErrBusy, expected: controller.RejectionBusy},
		{name: "invalid name", kind: controller.CommandSetSessionName, sessionID: mo.None[session.ID](), sessionName: mo.Some("invalid"), operationErr: session.ErrInvalidName, expected: controller.RejectionInvalidArgument},
		{name: "unknown ID", kind: controller.CommandResumeSession, sessionID: mo.Some(session.ID("missing")), sessionName: mo.None[string](), operationErr: os.ErrNotExist, expected: controller.RejectionNotFound},
		{name: "persistence", kind: controller.CommandSetSessionName, sessionID: mo.None[session.ID](), sessionName: mo.Some("name"), operationErr: errors.New("disk failed"), expected: controller.RejectionInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			control := NewMockSessionControl(gomock.NewController(t))
			switch test.kind {
			case controller.CommandCreateSession:
				control.EXPECT().Create(gomock.Any()).Return(session.Info{}, test.operationErr)
			case controller.CommandResumeSession:
				control.EXPECT().Resume(gomock.Any(), test.sessionID.MustGet()).Return(session.Info{}, test.operationErr)
			case controller.CommandSetSessionName:
				control.EXPECT().SetName(gomock.Any(), test.sessionName.MustGet()).Return(session.Info{}, test.operationErr)
			case controller.CommandUnspecified, controller.CommandUserRequest, controller.CommandAbort,
				controller.CommandGetRunState, controller.CommandGetMessages, controller.CommandGetModels,
				controller.CommandSelectModel, controller.CommandSelectReasoningChoice,
				controller.CommandListSessions, controller.CommandGetSessionInfo:
				t.Fatalf("unsupported command kind %d", test.kind)
			}
			service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
			response, operation, err := service.Handle(t.Context(), controller.Command{
				CorrelationID: test.name, Kind: test.kind, UserText: mo.None[string](),
				ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: test.sessionID,
				SessionName: test.sessionName,
			})
			require.NoError(t, err)
			assert.Nil(t, operation)
			assert.Equal(t, test.expected, response.Rejection.MustGet().Code)
		})
	}
}

func TestSessionLifecycleCommands(t *testing.T) {
	t.Parallel()

	info := session.Info{
		ID:               "session-id",
		Name:             mo.Some("named"),
		WorkingDirectory: "/project",
		StoragePath:      mo.Some("/sessions/session-id.jsonl"),
		CreatedAt:        time.Unix(1, 0),
		UpdatedAt:        time.Unix(2, 0),
	}
	tests := []struct {
		name         string
		kind         controller.CommandKind
		sessionID    mo.Option[session.ID]
		sessionName  mo.Option[string]
		expectedKind controller.ResponseKind
		expect       func(*MockSessionControl)
	}{
		{
			name: "create", kind: controller.CommandCreateSession,
			sessionID: mo.None[session.ID](), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessionInfo,
			expect:       func(control *MockSessionControl) { control.EXPECT().Create(gomock.Any()).Return(info, nil) },
		},
		{
			name: "list", kind: controller.CommandListSessions,
			sessionID: mo.None[session.ID](), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessions,
			expect: func(control *MockSessionControl) {
				control.EXPECT().List(gomock.Any()).Return([]session.Summary{{
					Info: info, FirstUserText: mo.None[string](), TotalMessages: 0,
				}}, nil)
			},
		},
		{
			name: "resume", kind: controller.CommandResumeSession,
			sessionID: mo.Some(session.ID("session-id")), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessionInfo,
			expect: func(control *MockSessionControl) {
				control.EXPECT().Resume(gomock.Any(), session.ID("session-id")).Return(info, nil)
			},
		},
		{
			name: "name", kind: controller.CommandSetSessionName,
			sessionID: mo.None[session.ID](), sessionName: mo.Some("named"),
			expectedKind: controller.ResponseSessionInfo,
			expect: func(control *MockSessionControl) {
				control.EXPECT().SetName(gomock.Any(), "named").Return(info, nil)
			},
		},
		{
			name: "information", kind: controller.CommandGetSessionInfo,
			sessionID: mo.None[session.ID](), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessionInfo,
			expect:       func(control *MockSessionControl) { control.EXPECT().Info().Return(info) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			control := NewMockSessionControl(gomock.NewController(t))
			test.expect(control)
			service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
			response, operation, err := service.Handle(t.Context(), controller.Command{
				CorrelationID:   test.name,
				Kind:            test.kind,
				UserText:        mo.None[string](),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       test.sessionID,
				SessionName:     test.sessionName,
			})
			require.NoError(t, err)
			assert.Nil(t, operation)
			assert.Equal(t, test.expectedKind, response.Kind)
		})
	}
}

// TestAcceptedOperationStartsExplicitlyAndBackpressures verifies the returned operation contract.
func (s *ServiceSuite) TestAcceptedOperationStartsExplicitlyAndBackpressures() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)
		started := make(chan struct{})
		delivered := make(chan struct{})
		coordinator.EXPECT().PrepareRun().Return("run-1", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-1", "request").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				close(started)
				if err := delivery.DeliverAgent(ctx, run.Event{Type: run.EventAgentStart, RunID: "run-1", Position: mo.None[int](), Content: mo.None[model.Content](), Message: mo.None[model.Response](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Progress: mo.None[tool.Progress](), ToolResult: mo.None[agent.ToolResult](), Turn: mo.None[run.TurnSummary](), Agent: mo.None[run.AgentSummary]()}); err != nil {
					return agent.RunOutcomeFailed, err
				}
				close(delivered)
				if err := delivery.DeliverSettled(context.WithoutCancel(ctx), "run-1"); err != nil {
					return agent.RunOutcomeFailed, err
				}
				return agent.RunOutcomeCompleted, nil
			},
		)

		response, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "c1", Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})

		require.NoError(t, err)
		assert.Equal(t, controller.Response{
			CorrelationID: "c1", Kind: controller.ResponseUserRequestAccepted, State: mo.None[controller.RunStateResult](), Messages: nil, Models: mo.None[controller.ModelsResult](), Selection: mo.None[model.Selection](), Rejection: mo.None[controller.Rejection](),
			SessionInfo: mo.None[session.Info](),
			Sessions:    nil,
		}, response)
		require.NotNil(t, operation)
		select {
		case <-started:
			assert.Fail(t, "operation started before Start")
		default:
		}

		operation.Start()
		synctest.Wait()
		select {
		case <-started:
		default:
			assert.Fail(t, "operation did not start")
		}
		select {
		case <-delivered:
			assert.Fail(t, "event production did not apply backpressure")
		default:
		}

		assert.Equal(t, controller.AgentEvent{
			CorrelationID: "c1", Type: controller.AgentEventAgentStart, RunID: "run-1", ModelContent: mo.None[controller.ModelContent](), ToolCallPreview: mo.None[controller.ToolCallPreview](), FinalToolCall: mo.None[controller.FinalToolCall](), ToolExecution: mo.None[controller.ToolExecution](), ToolProgress: mo.None[controller.ToolProgress](), ToolResult: mo.None[controller.ToolResult](), ModelResponse: mo.None[controller.ModelResponse](), Turn: mo.None[controller.TurnSummary](), Agent: mo.None[controller.AgentSummary](),
		}, <-operation.Events())
		synctest.Wait()
		select {
		case <-delivered:
		default:
			assert.Fail(t, "event producer remained blocked after consumption")
		}
		assert.Equal(t, controller.AgentEvent{
			CorrelationID: "c1", Type: controller.AgentEventAgentSettled, RunID: "run-1", ModelContent: mo.None[controller.ModelContent](), ToolCallPreview: mo.None[controller.ToolCallPreview](), FinalToolCall: mo.None[controller.FinalToolCall](), ToolExecution: mo.None[controller.ToolExecution](), ToolProgress: mo.None[controller.ToolProgress](), ToolResult: mo.None[controller.ToolResult](), ModelResponse: mo.None[controller.ModelResponse](), Turn: mo.None[controller.TurnSummary](), Agent: mo.None[controller.AgentSummary](),
		}, <-operation.Events())
		synctest.Wait()
		_, open := <-operation.Events()
		assert.False(t, open)
	})
}

// TestSequentialRunsKeepPreparedRunIDs verifies a settled session accepts later work.
func (s *ServiceSuite) TestSequentialRunsKeepPreparedRunIDs() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)

		for index, values := range []struct {
			correlationID string
			runID         string
		}{
			{correlationID: "c1", runID: "run-1"},
			{correlationID: "c2", runID: "run-2"},
		} {
			coordinator.EXPECT().PrepareRun().Return(values.runID, nil)
			coordinator.EXPECT().RunPrepared(gomock.Any(), values.runID, "request").DoAndReturn(
				func(ctx context.Context, runID, _ string) (agent.RunOutcome, error) {
					if err := delivery.DeliverSettled(context.WithoutCancel(ctx), runID); err != nil {
						return agent.RunOutcomeFailed, err
					}
					return agent.RunOutcomeCompleted, nil
				},
			)
			response, operation, err := service.Handle(t.Context(), controller.Command{
				CorrelationID: values.correlationID, Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			})
			require.NoError(t, err)
			assert.Equal(t, controller.ResponseUserRequestAccepted, response.Kind)
			require.NotNil(t, operation)
			operation.Start()
			event := <-operation.Events()
			assert.Equal(t, values.runID, event.RunID)
			assert.Equal(t, values.correlationID, event.CorrelationID)
			synctest.Wait()
			_, open := <-operation.Events()
			assert.False(t, open, "run %d event stream remained open", index)
		}
	})
}

// TestCommandRejectionPrecedence verifies first-match evaluation for overlapping failures.
func (s *ServiceSuite) TestCommandRejectionPrecedence() {
	tests := []struct {
		name         string
		active       bool
		command      controller.Command
		prepareErr   error
		expectedCode controller.RejectionCode
		expectedType controller.CommandKind
	}{
		{
			name: "missing payload precedes active correlation and busy state", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandUnspecified, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandUnspecified, prepareErr: nil,
		},
		{
			name: "blank user request precedes active correlation and busy state", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some(" \t"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandUserRequest, prepareErr: nil,
		},
		{
			name: "unexpected query payload precedes active correlation", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandGetRunState, UserText: mo.Some("unexpected"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandGetRunState, prepareErr: nil,
		},
		{
			name: "active correlation precedes busy state", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("next"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionCorrelationInUse, expectedType: controller.CommandUserRequest, prepareErr: nil,
		},
		{
			name: "busy state precedes allocation failure", active: true,
			command: controller.Command{CorrelationID: "other", Kind: controller.CommandUserRequest, UserText: mo.Some("next"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			prepareErr: errors.New("must not allocate"), expectedCode: controller.RejectionBusy,
			expectedType: controller.CommandUserRequest,
		},
		{
			name: "abort without active run", command: controller.Command{CorrelationID: "abort", Kind: controller.CommandAbort, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionNoActiveRun, expectedType: controller.CommandAbort, active: false, prepareErr: nil,
		},
		{
			name: "allocation failure after valid idle request",
			command: controller.Command{CorrelationID: "request", Kind: controller.CommandUserRequest, UserText: mo.Some("next"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			prepareErr: errors.New("entropy failed"), expectedCode: controller.RejectionInternal,
			expectedType: controller.CommandUserRequest, active: false,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			ctrl := gomock.NewController(s.T())
			coordinator := NewMockCoordinator(ctrl)
			coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
			service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
			if test.active {
				coordinator.EXPECT().PrepareRun().Return("run-active", nil)
				_, operation, err := service.Handle(s.T().Context(), controller.Command{
					CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
					SessionID:   mo.None[session.ID](),
					SessionName: mo.None[string](),
				})
				s.Require().NoError(err)
				s.Require().NotNil(operation)
				defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()
			}
			if test.expectedCode == controller.RejectionInternal {
				coordinator.EXPECT().PrepareRun().Return("", test.prepareErr)
			}

			response, operation, err := service.Handle(s.T().Context(), test.command)

			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(test.command.CorrelationID, response.CorrelationID)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(test.expectedType, response.Rejection.OrEmpty().Command)
			s.Equal(test.expectedCode, response.Rejection.OrEmpty().Code)
		})
	}
}

// TestModelCommandsUseCatalogDuringActiveRun verifies independent catalog commands.
func (s *ServiceSuite) TestModelCommandsUseCatalogDuringActiveRun() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	catalog := NewMockModelCatalog(ctrl)
	service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)
	_, activeOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	s.Require().NoError(err)
	s.Require().NotNil(activeOperation)
	defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()

	type contextKey struct{}
	commandContext := context.WithValue(s.T().Context(), contextKey{}, "selection")
	models := []model.Descriptor{{
		Provider: "provider", Model: "model",
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
			Default: model.ReasoningChoiceLow,
		}, ToolCapabilities: model.ToolCapabilities{},
	}}
	initial := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	selectedModel := model.Selection{Provider: "other", Model: "next", ReasoningChoice: model.ReasoningChoiceLow}
	selectedReasoning := model.Selection{Provider: "other", Model: "next", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog.EXPECT().Models().Return(models)
	catalog.EXPECT().Selection().Return(initial)
	catalog.EXPECT().SelectModel(gomock.Eq(commandContext), model.ProviderID("other"), model.ID("next")).Return(selectedModel, nil)
	catalog.EXPECT().SelectReasoningChoice(model.ReasoningChoiceHigh).Return(selectedReasoning, nil)

	tests := []struct {
		command controller.Command
		want    controller.Response
	}{
		{
			command: controller.Command{CorrelationID: "models", Kind: controller.CommandGetModels, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			want: controller.Response{
				CorrelationID: "models", Kind: controller.ResponseModels,
				Models: mo.Some(controller.ModelsResult{Models: models, ActiveSelection: mo.Some(initial)}), State: mo.None[controller.RunStateResult](), Messages: nil, Selection: mo.None[model.Selection](), Rejection: mo.None[controller.Rejection](),
				SessionInfo: mo.None[session.Info](),
				Sessions:    nil,
			},
		},
		{
			command: controller.Command{
				CorrelationID: "model", Kind: controller.CommandSelectModel, ProviderID: mo.Some(model.ProviderID("other")), ModelID: mo.Some(model.ID("next")), UserText: mo.None[string](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			},
			want: controller.Response{
				CorrelationID: "model", Kind: controller.ResponseModelSelection, Selection: mo.Some(selectedModel), State: mo.None[controller.RunStateResult](), Messages: nil, Models: mo.None[controller.ModelsResult](), Rejection: mo.None[controller.Rejection](),
				SessionInfo: mo.None[session.Info](),
				Sessions:    nil,
			},
		},
		{
			command: controller.Command{
				CorrelationID: "reasoning", Kind: controller.CommandSelectReasoningChoice,
				ReasoningChoice: mo.Some(model.ReasoningChoiceHigh), UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			},
			want: controller.Response{
				CorrelationID: "reasoning", Kind: controller.ResponseModelSelection, Selection: mo.Some(selectedReasoning), State: mo.None[controller.RunStateResult](), Messages: nil, Models: mo.None[controller.ModelsResult](), Rejection: mo.None[controller.Rejection](),
				SessionInfo: mo.None[session.Info](),
				Sessions:    nil,
			},
		},
	}
	for _, test := range tests {
		response, operation, handleErr := service.Handle(commandContext, test.command)
		s.Require().NoError(handleErr)
		s.Nil(operation)
		s.Equal(test.want, response)
	}
}

// TestInvalidModelCommandsDoNotCallCatalog verifies argument validation before selection.
func (s *ServiceSuite) TestInvalidModelCommandsDoNotCallCatalog() {
	ctrl := gomock.NewController(s.T())
	service := New(
		NewMockCoordinator(ctrl), NewMockModelCatalog(ctrl),
		idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery(),
	)

	commands := []controller.Command{
		{CorrelationID: "provider", Kind: controller.CommandSelectModel, ModelID: mo.Some(model.ID("model")), UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
		{CorrelationID: "model", Kind: controller.CommandSelectModel, ProviderID: mo.Some(model.ProviderID("provider")), UserText: mo.None[string](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
		{CorrelationID: "reasoning", Kind: controller.CommandSelectReasoningChoice, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
	}
	for _, command := range commands {
		response, operation, err := service.Handle(s.T().Context(), command)
		s.Require().NoError(err)
		s.Nil(operation)
		s.Equal(controller.ResponseRejected, response.Kind)
		s.Equal(controller.RejectionInvalidArgument, response.Rejection.OrEmpty().Code)
		s.Equal(command.Kind, response.Rejection.OrEmpty().Command)
	}
}

// TestSelectionErrorsMapToSafeRejections verifies the catalog error boundary.
func (s *ServiceSuite) TestSelectionErrorsMapToSafeRejections() {
	tests := []struct {
		name string
		err  error
		code controller.RejectionCode
	}{
		{
			name: "not found",
			err:  selectionError{code: SelectionNotFound},
			code: controller.RejectionNotFound,
		},
		{
			name: "reasoning unsupported",
			err:  selectionError{code: SelectionReasoningUnsupported},
			code: controller.RejectionReasoningUnsupported,
		},
		{
			name: "credential unavailable",
			err:  selectionError{code: SelectionCredentialUnavailable},
			code: controller.RejectionCredentialUnavailable,
		},
		{name: "internal", err: errors.New("internal details"), code: controller.RejectionInternal},
	}
	for _, test := range tests {
		s.Run(test.name, func() {
			ctrl := gomock.NewController(s.T())
			catalog := NewMockModelCatalog(ctrl)
			service := New(
				NewMockCoordinator(ctrl), catalog,
				idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery(),
			)
			catalog.EXPECT().SelectModel(gomock.Any(), model.ProviderID("provider"), model.ID("model")).Return(model.Selection{}, test.err)

			response, operation, err := service.Handle(s.T().Context(), controller.Command{
				CorrelationID: "selection", Kind: controller.CommandSelectModel,
				ProviderID: mo.Some(model.ProviderID("provider")), ModelID: mo.Some(model.ID("model")), UserText: mo.None[string](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			})
			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(test.code, response.Rejection.OrEmpty().Code)
			if test.code == controller.RejectionInternal {
				s.NotContains(response.Rejection.OrEmpty().Message, "internal details")
			}
		})
	}
}

// TestConcurrentReservationRejectsOneRequest verifies active reservation is atomic.
func (s *ServiceSuite) TestConcurrentReservationRejectsOneRequest() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	var prepareBarrier sync.WaitGroup
	prepareBarrier.Add(2)
	var runNumber atomic.Int64
	coordinator.EXPECT().PrepareRun().DoAndReturn(func() (string, error) {
		prepareBarrier.Done()
		prepareBarrier.Wait()
		if runNumber.Add(1) == 1 {
			return "run-1", nil
		}
		return "run-2", nil
	}).Times(2)

	type result struct {
		response  controller.Response
		operation controller.Operation
		err       error
	}
	results := make([]result, 2)
	var calls sync.WaitGroup
	calls.Add(2)
	for index := range results {
		go func() {
			defer calls.Done()
			results[index].response, results[index].operation, results[index].err = service.Handle(
				s.T().Context(), controller.Command{
					CorrelationID: string(rune('a' + index)), Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
					SessionID:   mo.None[session.ID](),
					SessionName: mo.None[string](),
				},
			)
		}()
	}
	calls.Wait()

	accepted := 0
	rejected := 0
	for _, result := range results {
		s.Require().NoError(result.err)
		switch result.response.Kind {
		case controller.ResponseUserRequestAccepted:
			accepted++
			s.Require().NotNil(result.operation)
		case controller.ResponseRejected:
			rejected++
			s.Equal(controller.RejectionBusy, result.response.Rejection.OrEmpty().Code)
		case controller.ResponseUnspecified,
			controller.ResponseAbortCompleted,
			controller.ResponseRunState,
			controller.ResponseMessages,
			controller.ResponseModels,
			controller.ResponseModelSelection,
			controller.ResponseSessionInfo,
			controller.ResponseSessions:
			s.Fail("unexpected response", "kind %d", result.response.Kind)
		}
	}
	s.Equal(1, accepted)
	s.Equal(1, rejected)
	s.Require().NoError(service.CancelAndWait(s.T().Context()))
}

// TestDisconnectPreventsLateReservation verifies in-flight acceptance cannot outlive session cleanup.
func (s *ServiceSuite) TestDisconnectPreventsLateReservation() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	preparing := make(chan struct{})
	prepared := make(chan struct{})
	coordinator.EXPECT().PrepareRun().DoAndReturn(func() (string, error) {
		close(preparing)
		<-prepared
		return "run-late", nil
	})
	type handleResult struct {
		response  controller.Response
		operation controller.Operation
		err       error
	}
	result := make(chan handleResult)
	go func() {
		response, operation, err := service.Handle(s.T().Context(), controller.Command{
			CorrelationID: "late", Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})
		result <- handleResult{response: response, operation: operation, err: err}
	}()
	<-preparing

	s.Require().NoError(service.CancelAndWait(s.T().Context()))
	close(prepared)
	handled := <-result

	s.Require().NoError(handled.err)
	s.Nil(handled.operation)
	s.Equal(controller.ResponseRejected, handled.response.Kind)
	s.Equal(controller.RejectionBusy, handled.response.Rejection.OrEmpty().Code)
}

// TestQueriesReturnPublicSnapshotsDuringAcceptedRun verifies state correlation and history mapping.
func (s *ServiceSuite) TestQueriesReturnPublicSnapshotsDuringAcceptedRun() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	delivery := NewDelivery()
	state := run.State{
		Status: run.StatusRunning, RunID: mo.Some("run-active"),
		PartialResponse: mo.Some(model.Response{Content: []model.Content{{Kind: model.ContentText, Text: mo.Some("partial"), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}}, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}),
		ToolPreviews:    map[string]model.ToolCallPreview{"preview": {CallID: "preview", Name: "", Position: 0, Provisional: false, Fields: nil}},
	}
	var history []agent.HistoryEntry
	service := New(coordinator, nil, func() run.State { return state }, func() []agent.HistoryEntry { return history }, nil, delivery)
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)
	_, operation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	s.Require().NoError(err)
	s.Require().NotNil(operation)
	defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()

	response, returnedOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "state", Kind: controller.CommandGetRunState, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.Response{
		CorrelationID: "state", Kind: controller.ResponseRunState,
		State: mo.Some(controller.RunStateResult{State: controller.RunStateRunning, ActiveCorrelationID: mo.Some("active")}), Messages: nil, Models: mo.None[controller.ModelsResult](), Selection: mo.None[model.Selection](), Rejection: mo.None[controller.Rejection](),
		SessionInfo: mo.None[session.Info](),
		Sessions:    nil,
	}, response)

	responseModel := model.ID("response-model")
	history = []agent.HistoryEntry{
		{Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("hello")), Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult]()},
		{Kind: agent.HistoryEntryModel, Model: mo.Some(model.Response{
			Content: []model.Content{
				{Kind: model.ContentText, Text: mo.Some("answer"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentText, Text: mo.Some("partial"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentReasoning, ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "provider", API: "", Model: "", CompatibilityKey: mo.None[string]()}, Payload: []byte(`{"secret":true}`)}), Text: mo.None[string](), Final: true, ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentReasoning, Text: mo.Some("reason"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
			},
			Outcome: mo.Some(model.OutcomeStop), Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")), ResponseModel: mo.Some(responseModel), ErrorMessage: mo.None[string](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		}), User: mo.None[model.Message](), ToolResult: mo.None[agent.ToolResult](),
		},
		{Kind: agent.HistoryEntryToolResult, ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "tool",
			Contents: []tool.ResultContent{
				{Kind: tool.ResultContentText, Text: mo.Some("output"), Image: mo.None[tool.ResultImage]()},
				{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}})},
			}, IsError: false,
		}), User: mo.None[model.Message](), Model: mo.None[model.Response](),
		},
	}
	response, returnedOperation, err = service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "messages", Kind: controller.CommandGetMessages, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.ResponseMessages, response.Kind)
	s.Require().Len(response.Messages, 3)
	s.Equal("hello", response.Messages[0].UserText.OrEmpty())
	modelResponse := response.Messages[1].Model.OrEmpty()
	s.Equal("answerpartial", modelResponse.Text)
	s.Require().Len(modelResponse.Content, 3)
	s.Equal(controller.ModelResponseContentReasoning, modelResponse.Content[2].Kind)
	toolResult := response.Messages[2].ToolResult.OrEmpty()
	image, present := toolResult.Contents[1].Image.Get()
	s.Require().True(present)
	s.Equal([]byte{1, 2}, image.Data)
	image.Data[0] = 9
	s.Equal(byte(1), history[2].ToolResult.OrEmpty().Contents[1].Image.OrEmpty().Data[0])
}

// TestAbortCancelsAcceptedOperationWithoutStarting verifies accepted work can be released before Start.
func (s *ServiceSuite) TestAbortCancelsAcceptedOperationWithoutStarting() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	coordinator.EXPECT().PrepareRun().Return("run-accepted", nil)
	_, operation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	s.Require().NoError(err)

	response, returnedOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "abort", Kind: controller.CommandAbort, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})

	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.ResponseAbortCompleted, response.Kind)
	_, open := <-operation.Events()
	s.False(open)
	operation.Start()
}

// TestAbortCancelsJoinsAndReportsIdle verifies settlement precedes abort completion.
func (s *ServiceSuite) TestAbortCancelsJoinsAndReportsIdle() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)
		started := make(chan struct{})
		var runContextErr error
		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				close(started)
				<-ctx.Done()
				runContextErr = ctx.Err()
				if err := delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"); err != nil {
					return agent.RunOutcomeFailed, err
				}
				return agent.RunOutcomeAborted, context.Canceled
			},
		)
		_, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})
		require.NoError(t, err)
		operation.Start()
		synctest.Wait()
		select {
		case <-started:
		default:
			assert.Fail(t, "run did not start")
		}

		type abortResult struct {
			response controller.Response
			err      error
		}
		aborted := make(chan abortResult)
		go func() {
			response, _, abortErr := service.Handle(t.Context(), controller.Command{
				CorrelationID: "abort", Kind: controller.CommandAbort, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			})
			aborted <- abortResult{response: response, err: abortErr}
		}()
		synctest.Wait()
		select {
		case result := <-aborted:
			assert.Fail(t, "abort completed before settlement", "result: %+v", result)
		default:
		}
		assert.Equal(t, controller.AgentEventAgentSettled, (<-operation.Events()).Type)
		synctest.Wait()
		result := <-aborted
		require.NoError(t, result.err)
		assert.Equal(t, controller.ResponseAbortCompleted, result.response.Kind)
		require.ErrorIs(t, runContextErr, context.Canceled)

		state, _, stateErr := service.Handle(t.Context(), controller.Command{
			CorrelationID: "state", Kind: controller.CommandGetRunState, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})
		require.NoError(t, stateErr)
		assert.Equal(t, controller.RunStateIdle, state.State.OrEmpty().State)
	})
}

// TestAbortPreservesJoinedNonCancellationError verifies cleanup failures prevent false success.
func (s *ServiceSuite) TestAbortPreservesJoinedNonCancellationError() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)
		cleanupErr := errors.New("settlement delivery failed")
		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				<-ctx.Done()
				if err := delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"); err != nil {
					return agent.RunOutcomeFailed, err
				}
				return agent.RunOutcomeAborted, errors.Join(context.Canceled, cleanupErr)
			},
		)
		_, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})
		require.NoError(t, err)
		operation.Start()
		synctest.Wait()
		aborted := make(chan error)
		go func() {
			_, _, abortErr := service.Handle(t.Context(), controller.Command{
				CorrelationID: "abort", Kind: controller.CommandAbort, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			})
			aborted <- abortErr
		}()
		synctest.Wait()
		assert.Equal(t, controller.AgentEventAgentSettled, (<-operation.Events()).Type)
		synctest.Wait()

		abortErr := <-aborted
		require.ErrorIs(t, abortErr, cleanupErr)
		require.NotErrorIs(t, abortErr, context.Canceled)
	})
}

// TestDisconnectCancelsBlockedEventAndJoins verifies stream closure releases synchronous production.
func (s *ServiceSuite) TestDisconnectCancelsBlockedEventAndJoins() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)
		started := make(chan struct{})
		var runContextErr error
		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				close(started)
				deliveryErr := delivery.DeliverAgent(context.WithoutCancel(ctx), run.Event{
					Type: run.EventAgentStart, RunID: "run-active", Position: mo.None[int](), Content: mo.None[model.Content](), Message: mo.None[model.Response](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Progress: mo.None[tool.Progress](), ToolResult: mo.None[agent.ToolResult](), Turn: mo.None[run.TurnSummary](), Agent: mo.None[run.AgentSummary](),
				})
				runContextErr = ctx.Err()
				return agent.RunOutcomeAborted, errors.Join(context.Canceled, deliveryErr)
			},
		)
		_, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})
		require.NoError(t, err)
		operation.Start()
		synctest.Wait()
		select {
		case <-started:
		default:
			assert.Fail(t, "run did not reach event production")
		}

		require.NoError(t, service.CancelAndWait(t.Context()))
		require.ErrorIs(t, runContextErr, context.Canceled)
		_, open := <-operation.Events()
		assert.False(t, open)
	})
}

// TestDisconnectJoinsRunAfterSettlement verifies idle delivery retains the join handle.
func (s *ServiceSuite) TestDisconnectJoinsRunAfterSettlement() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)
		release := make(chan struct{})
		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				if err := delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"); err != nil {
					return agent.RunOutcomeFailed, err
				}
				<-release
				return agent.RunOutcomeCompleted, nil
			},
		)
		_, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](),
		})
		require.NoError(t, err)
		operation.Start()
		assert.Equal(t, controller.AgentEventAgentSettled, (<-operation.Events()).Type)
		synctest.Wait()

		completed := make(chan error)
		go func() { completed <- service.CancelAndWait(t.Context()) }()
		synctest.Wait()
		select {
		case disconnectErr := <-completed:
			assert.Fail(t, "disconnect returned before run completion", "error: %v", disconnectErr)
		default:
		}
		close(release)
		synctest.Wait()
		require.NoError(t, <-completed)
	})
}

// TestEmptyCorrelationReturnsTerminalError verifies uncorrelated commands produce no response.
func (s *ServiceSuite) TestEmptyCorrelationReturnsTerminalError() {
	ctrl := gomock.NewController(s.T())
	service := New(NewMockCoordinator(ctrl), nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())

	response, operation, err := service.Handle(
		s.T().Context(), controller.Command{Kind: controller.CommandGetRunState, CorrelationID: "", UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
	)

	s.Require().ErrorIs(err, ErrCorrelationRequired)
	s.Equal(controller.Response{}, response)
	s.Nil(operation)
}

func idleStateSnapshot() run.State {
	return run.State{}
}

func emptyHistorySnapshot() []agent.HistoryEntry {
	return nil
}
