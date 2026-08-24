//nolint:exhaustruct // Tests set only fields relevant to each command and event.
package programmatic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
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

// TestAcceptedOperationStartsExplicitlyAndBackpressures verifies the returned operation contract.
func (s *ServiceSuite) TestAcceptedOperationStartsExplicitlyAndBackpressures() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, delivery)
		started := make(chan struct{})
		delivered := make(chan struct{})
		coordinator.EXPECT().PrepareRun().Return("run-1", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-1", "request").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				close(started)
				if err := delivery.DeliverAgent(ctx, run.Event{Type: run.EventAgentStart, RunID: "run-1"}); err != nil {
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
			CorrelationID: "c1", Kind: controller.CommandUserRequest, UserText: "request",
		})

		require.NoError(t, err)
		assert.Equal(t, controller.Response{
			CorrelationID: "c1", Kind: controller.ResponseUserRequestAccepted,
		}, response)
		require.NotNil(t, operation)
		select {
		case <-started:
			assert.Fail(t, "operation started before Start")
		default:
		}

		operation.Start()
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
			CorrelationID: "c1", Type: controller.AgentEventAgentStart, RunID: "run-1",
		}, <-operation.Events())
		synctest.Wait()
		select {
		case <-delivered:
		default:
			assert.Fail(t, "event producer remained blocked after consumption")
		}
		assert.Equal(t, controller.AgentEvent{
			CorrelationID: "c1", Type: controller.AgentEventAgentSettled, RunID: "run-1",
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
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, delivery)

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
				CorrelationID: values.correlationID, Kind: controller.CommandUserRequest, UserText: "request",
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
			command:      controller.Command{CorrelationID: "active", Kind: controller.CommandUnspecified},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandUnspecified,
		},
		{
			name: "blank user request precedes active correlation and busy state", active: true,
			command:      controller.Command{CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: " \t"},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandUserRequest,
		},
		{
			name: "unexpected query payload precedes active correlation", active: true,
			command:      controller.Command{CorrelationID: "active", Kind: controller.CommandGetRunState, UserText: "unexpected"},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandGetRunState,
		},
		{
			name: "active correlation precedes busy state", active: true,
			command:      controller.Command{CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "next"},
			expectedCode: controller.RejectionCorrelationInUse, expectedType: controller.CommandUserRequest,
		},
		{
			name: "busy state precedes allocation failure", active: true,
			command:    controller.Command{CorrelationID: "other", Kind: controller.CommandUserRequest, UserText: "next"},
			prepareErr: errors.New("must not allocate"), expectedCode: controller.RejectionBusy,
			expectedType: controller.CommandUserRequest,
		},
		{
			name: "abort without active run", command: controller.Command{CorrelationID: "abort", Kind: controller.CommandAbort},
			expectedCode: controller.RejectionNoActiveRun, expectedType: controller.CommandAbort,
		},
		{
			name:       "allocation failure after valid idle request",
			command:    controller.Command{CorrelationID: "request", Kind: controller.CommandUserRequest, UserText: "next"},
			prepareErr: errors.New("entropy failed"), expectedCode: controller.RejectionInternal,
			expectedType: controller.CommandUserRequest,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			ctrl := gomock.NewController(s.T())
			coordinator := NewMockCoordinator(ctrl)
			service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, NewDelivery())
			if test.active {
				coordinator.EXPECT().PrepareRun().Return("run-active", nil)
				_, operation, err := service.Handle(s.T().Context(), controller.Command{
					CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
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
			s.Equal(test.expectedType, response.Rejection.Command)
			s.Equal(test.expectedCode, response.Rejection.Code)
		})
	}
}

// TestModelCommandsUseCatalogDuringActiveRun verifies independent catalog commands.
func (s *ServiceSuite) TestModelCommandsUseCatalogDuringActiveRun() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	catalog := NewMockModelCatalog(ctrl)
	service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, NewDelivery())
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)
	_, activeOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "request",
	})
	s.Require().NoError(err)
	s.Require().NotNil(activeOperation)
	defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()

	type contextKey struct{}
	commandContext := context.WithValue(s.T().Context(), contextKey{}, "selection")
	models := []model.Descriptor{{
		Provider: "provider", Model: "model",
		SupportedReasoningLevels: []model.ReasoningLevel{model.ReasoningLevelLow, model.ReasoningLevelHigh},
	}}
	initial := model.Selection{Provider: "provider", Model: "model", ReasoningLevel: model.ReasoningLevelLow}
	selectedModel := model.Selection{Provider: "other", Model: "next", ReasoningLevel: model.ReasoningLevelLow}
	selectedReasoning := model.Selection{Provider: "other", Model: "next", ReasoningLevel: model.ReasoningLevelHigh}
	catalog.EXPECT().Models().Return(models)
	catalog.EXPECT().Selection().Return(initial)
	catalog.EXPECT().SelectModel(gomock.Eq(commandContext), model.ProviderID("other"), model.ID("next")).Return(selectedModel, nil)
	catalog.EXPECT().SelectReasoningLevel(model.ReasoningLevelHigh).Return(selectedReasoning, nil)

	tests := []struct {
		command controller.Command
		want    controller.Response
	}{
		{
			command: controller.Command{CorrelationID: "models", Kind: controller.CommandGetModels},
			want: controller.Response{
				CorrelationID: "models", Kind: controller.ResponseModels,
				Models: controller.ModelsResult{Models: models, ActiveSelection: initial},
			},
		},
		{
			command: controller.Command{
				CorrelationID: "model", Kind: controller.CommandSelectModel, ProviderID: "other", ModelID: "next",
			},
			want: controller.Response{
				CorrelationID: "model", Kind: controller.ResponseModelSelection, Selection: selectedModel,
			},
		},
		{
			command: controller.Command{
				CorrelationID: "reasoning", Kind: controller.CommandSelectReasoningLevel,
				ReasoningLevel: model.ReasoningLevelHigh,
			},
			want: controller.Response{
				CorrelationID: "reasoning", Kind: controller.ResponseModelSelection, Selection: selectedReasoning,
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
		idleStateSnapshot, emptyHistorySnapshot, NewDelivery(),
	)

	commands := []controller.Command{
		{CorrelationID: "provider", Kind: controller.CommandSelectModel, ModelID: "model"},
		{CorrelationID: "model", Kind: controller.CommandSelectModel, ProviderID: "provider"},
		{CorrelationID: "reasoning", Kind: controller.CommandSelectReasoningLevel},
	}
	for _, command := range commands {
		response, operation, err := service.Handle(s.T().Context(), command)
		s.Require().NoError(err)
		s.Nil(operation)
		s.Equal(controller.ResponseRejected, response.Kind)
		s.Equal(controller.RejectionInvalidArgument, response.Rejection.Code)
		s.Equal(command.Kind, response.Rejection.Command)
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
				idleStateSnapshot, emptyHistorySnapshot, NewDelivery(),
			)
			catalog.EXPECT().SelectModel(gomock.Any(), model.ProviderID("provider"), model.ID("model")).Return(model.Selection{}, test.err)

			response, operation, err := service.Handle(s.T().Context(), controller.Command{
				CorrelationID: "selection", Kind: controller.CommandSelectModel,
				ProviderID: "provider", ModelID: "model",
			})
			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(test.code, response.Rejection.Code)
			if test.code == controller.RejectionInternal {
				s.NotContains(response.Rejection.Message, "internal details")
			}
		})
	}
}

// TestConcurrentReservationRejectsOneRequest verifies active reservation is atomic.
func (s *ServiceSuite) TestConcurrentReservationRejectsOneRequest() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, NewDelivery())
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
					CorrelationID: string(rune('a' + index)), Kind: controller.CommandUserRequest, UserText: "request",
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
			s.Equal(controller.RejectionBusy, result.response.Rejection.Code)
		case controller.ResponseUnspecified,
			controller.ResponseAbortCompleted,
			controller.ResponseRunState,
			controller.ResponseMessages,
			controller.ResponseModels,
			controller.ResponseModelSelection:
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
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, NewDelivery())
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
			CorrelationID: "late", Kind: controller.CommandUserRequest, UserText: "request",
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
	s.Equal(controller.RejectionBusy, handled.response.Rejection.Code)
}

// TestQueriesReturnPublicSnapshotsDuringAcceptedRun verifies state correlation and history mapping.
func (s *ServiceSuite) TestQueriesReturnPublicSnapshotsDuringAcceptedRun() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	delivery := NewDelivery()
	state := run.State{
		Status: run.StatusRunning, RunID: "run-active",
		PartialResponse: model.Response{Content: []model.Content{{Kind: model.ContentText, Text: "partial"}}},
		ToolPreviews:    map[string]model.ToolCallPreview{"preview": {CallID: "preview"}},
	}
	var history []agent.HistoryEntry
	service := New(coordinator, nil, func() run.State { return state }, func() []agent.HistoryEntry { return history }, delivery)
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)
	_, operation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
	})
	s.Require().NoError(err)
	s.Require().NotNil(operation)
	defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()

	response, returnedOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "state", Kind: controller.CommandGetRunState,
	})
	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.Response{
		CorrelationID: "state", Kind: controller.ResponseRunState,
		State: controller.RunStateResult{State: controller.RunStateRunning, ActiveCorrelationID: "active"},
	}, response)

	responseModel := model.ID("response-model")
	history = []agent.HistoryEntry{
		{Kind: agent.HistoryEntryUser, User: model.TextMessage("hello")},
		{Kind: agent.HistoryEntryModel, Model: model.Response{
			Content: []model.Content{
				{Kind: model.ContentText, Text: "answer", Final: true},
				{Kind: model.ContentText, Text: "partial", Final: false},
				{Kind: model.ContentProviderContext, ProviderContext: model.ProviderContext{ProviderID: "provider", Payload: []byte(`{"secret":true}`)}},
				{Kind: model.ContentReasoning, Text: "reason", Final: true},
			},
			Outcome: model.OutcomeStop, Provider: "provider", Model: "model", ResponseModel: &responseModel,
		}},
		{Kind: agent.HistoryEntryToolResult, ToolResult: agent.ToolResult{
			CallID: "call", ToolName: "tool",
			Contents: []tool.ResultContent{
				{Kind: tool.ResultContentText, Text: "output"},
				{Kind: tool.ResultContentImage, Image: tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}},
			},
		}},
	}
	response, returnedOperation, err = service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "messages", Kind: controller.CommandGetMessages,
	})
	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.ResponseMessages, response.Kind)
	s.Require().Len(response.Messages, 3)
	s.Equal("hello", response.Messages[0].UserText)
	s.Equal("answer", response.Messages[1].Model.Text)
	s.Require().Len(response.Messages[1].Model.Content, 2)
	s.Equal(controller.ModelResponseContentReasoning, response.Messages[1].Model.Content[1].Kind)
	s.Equal([]byte{1, 2}, response.Messages[2].ToolResult.Contents[1].Image.Data)
	response.Messages[2].ToolResult.Contents[1].Image.Data[0] = 9
	s.Equal(byte(1), history[2].ToolResult.Contents[1].Image.Data[0])
}

// TestAbortCancelsAcceptedOperationWithoutStarting verifies accepted work can be released before Start.
func (s *ServiceSuite) TestAbortCancelsAcceptedOperationWithoutStarting() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, NewDelivery())
	coordinator.EXPECT().PrepareRun().Return("run-accepted", nil)
	_, operation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "request",
	})
	s.Require().NoError(err)

	response, returnedOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "abort", Kind: controller.CommandAbort,
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
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, delivery)
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
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
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
				CorrelationID: "abort", Kind: controller.CommandAbort,
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
			CorrelationID: "state", Kind: controller.CommandGetRunState,
		})
		require.NoError(t, stateErr)
		assert.Equal(t, controller.RunStateIdle, state.State.State)
	})
}

// TestAbortPreservesJoinedNonCancellationError verifies cleanup failures prevent false success.
func (s *ServiceSuite) TestAbortPreservesJoinedNonCancellationError() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, delivery)
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
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
		})
		require.NoError(t, err)
		operation.Start()
		synctest.Wait()
		aborted := make(chan error)
		go func() {
			_, _, abortErr := service.Handle(t.Context(), controller.Command{
				CorrelationID: "abort", Kind: controller.CommandAbort,
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
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, delivery)
		started := make(chan struct{})
		var runContextErr error
		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				close(started)
				deliveryErr := delivery.DeliverAgent(context.WithoutCancel(ctx), run.Event{
					Type: run.EventAgentStart, RunID: "run-active",
				})
				runContextErr = ctx.Err()
				return agent.RunOutcomeAborted, errors.Join(context.Canceled, deliveryErr)
			},
		)
		_, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
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
		delivery := NewDelivery()
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, delivery)
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
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
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
	service := New(NewMockCoordinator(ctrl), nil, idleStateSnapshot, emptyHistorySnapshot, NewDelivery())

	response, operation, err := service.Handle(
		s.T().Context(), controller.Command{Kind: controller.CommandGetRunState},
	)

	s.Require().ErrorIs(err, ErrCorrelationRequired)
	s.Equal(controller.Response{}, response)
	s.Nil(operation)
}

func idleStateSnapshot() run.State {
	return run.State{Status: run.StatusIdle}
}

func emptyHistorySnapshot() []agent.HistoryEntry {
	return nil
}
