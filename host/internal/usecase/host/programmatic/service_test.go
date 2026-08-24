//nolint:exhaustruct // Tests set only fields relevant to each command and event.
package programmatic

import (
	"context"
	"errors"
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

func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// TestAcceptedRunDeliversAcceptanceBeforeCorrelatedEvents verifies the accepted run lifecycle.
func (s *ServiceSuite) TestAcceptedRunDeliversAcceptanceBeforeCorrelatedEvents() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
		command := controller.Command{CorrelationID: "c1", Kind: controller.CommandUserRequest, UserText: "request"}

		gomock.InOrder(
			coordinator.EXPECT().PrepareRun().Return("run-1", nil),
			sender.EXPECT().SendResponse(gomock.Any(), controller.Response{
				CorrelationID: "c1",
				Kind:          controller.ResponseUserRequestAccepted,
			}),
			coordinator.EXPECT().RunPrepared(gomock.Any(), "run-1", "request").DoAndReturn(
				func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
					require.NoError(t, delivery.DeliverAgent(ctx, run.Event{Type: run.EventAgentStart, RunID: "run-1"}))
					require.NoError(t, delivery.DeliverSettled(ctx, "run-1"))
					return agent.RunOutcomeCompleted, nil
				},
			),
		)
		sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
			CorrelationID: "c1",
			Type:          controller.AgentEventAgentStart,
			RunID:         "run-1",
		})
		sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
			CorrelationID: "c1",
			Type:          controller.AgentEventAgentSettled,
			RunID:         "run-1",
		})

		err := service.Handle(t.Context(), command)
		synctest.Wait()

		assert.NoError(t, err)
	})
}

// TestCompletedRunReleasesPreparedContext verifies sequential runs release parent context references.
func (s *ServiceSuite) TestCompletedRunReleasesPreparedContext() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
		var runContext context.Context

		coordinator.EXPECT().PrepareRun().Return("run-complete", nil)
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
		sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
			CorrelationID: "complete", Type: controller.AgentEventAgentSettled, RunID: "run-complete",
		})
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-complete", "request").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				runContext = ctx
				require.NoError(t, delivery.DeliverSettled(context.WithoutCancel(ctx), "run-complete"))
				return agent.RunOutcomeCompleted, nil
			},
		)

		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "complete", Kind: controller.CommandUserRequest, UserText: "request",
		}))
		synctest.Wait()

		require.NotNil(t, runContext)
		require.ErrorIs(t, runContext.Err(), context.Canceled)
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
			synctest.Test(s.T(), func(t *testing.T) {
				ctrl := gomock.NewController(t)
				coordinator := NewMockCoordinator(ctrl)
				sender := NewMockSender(ctrl)
				delivery := NewDelivery(sender)
				service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
				if test.active {
					coordinator.EXPECT().PrepareRun().Return("run-active", nil)
					sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
					coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").Return(agent.RunOutcomeCompleted, nil)
					require.NoError(t, service.Handle(t.Context(), controller.Command{
						CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
					}))
					synctest.Wait()
				}
				if test.expectedCode == controller.RejectionInternal {
					coordinator.EXPECT().PrepareRun().Return("", test.prepareErr)
				}
				sender.EXPECT().SendResponse(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, response controller.Response) error {
						assert.Equal(t, test.command.CorrelationID, response.CorrelationID)
						assert.Equal(t, controller.ResponseRejected, response.Kind)
						assert.Equal(t, test.expectedType, response.Rejection.Command)
						assert.Equal(t, test.expectedCode, response.Rejection.Code)
						return nil
					},
				)

				err := service.Handle(t.Context(), test.command)
				assert.NoError(t, err)
			})
		})
	}
}

// TestConcurrentReservationRejectsTheLosingRequest verifies the busy response at the atomic reserve point.
func (s *ServiceSuite) TestConcurrentReservationRejectsTheLosingRequest() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	sender := NewMockSender(ctrl)
	delivery := NewDelivery(sender)
	service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
	coordinator.EXPECT().PrepareRun().DoAndReturn(func() (string, error) {
		s.Require().True(delivery.reserve(&activeRun{
			correlationID: "winner", runID: "run-winner", cancel: func() {}, done: make(chan struct{}),
		}))
		return "run-loser", nil
	})
	sender.EXPECT().SendResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response controller.Response) error {
			s.Equal("loser", response.CorrelationID)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(controller.RejectionBusy, response.Rejection.Code)
			return nil
		},
	)

	err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "loser", Kind: controller.CommandUserRequest, UserText: "request",
	})

	s.Require().NoError(err)
}

// TestQueriesReturnPublicSnapshotsDuringActiveRun verifies state correlation and ordered history mapping.
func (s *ServiceSuite) TestQueriesReturnPublicSnapshotsDuringActiveRun() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		state := run.State{
			Status: run.StatusRunning, RunID: "run-active",
			PartialResponse: model.Response{Content: []model.Content{{Kind: model.ContentText, Text: "partial"}}},
			ToolPreviews:    map[string]model.ToolCallPreview{"preview": {CallID: "preview"}},
		}
		var history []agent.HistoryEntry
		service := New(coordinator, func() run.State { return state }, func() []agent.HistoryEntry { return history }, delivery)
		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").Return(agent.RunOutcomeCompleted, nil)
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
		}))
		synctest.Wait()

		sender.EXPECT().SendResponse(gomock.Any(), controller.Response{
			CorrelationID: "state", Kind: controller.ResponseRunState,
			State: controller.RunStateResult{State: controller.RunStateRunning, ActiveCorrelationID: "active"},
		})
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "state", Kind: controller.CommandGetRunState,
		}))

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
				CallID: "call", ToolName: "tool", IsError: false,
				Contents: []tool.ResultContent{
					{Kind: tool.ResultContentText, Text: "output"},
					{Kind: tool.ResultContentImage, Image: tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}}},
				},
			}},
		}
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, response controller.Response) error {
				require.Equal(t, "messages", response.CorrelationID)
				require.Equal(t, controller.ResponseMessages, response.Kind)
				require.Len(t, response.Messages, 3)
				assert.Equal(t, "hello", response.Messages[0].UserText)
				assert.Equal(t, "answer", response.Messages[1].Model.Text)
				require.Len(t, response.Messages[1].Model.Content, 2)
				assert.Equal(t, controller.ModelResponseContentReasoning, response.Messages[1].Model.Content[1].Kind)
				assert.Equal(t, []byte{1, 2}, response.Messages[2].ToolResult.Contents[1].Image.Data)
				response.Messages[2].ToolResult.Contents[1].Image.Data[0] = 9
				return nil
			},
		)
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "messages", Kind: controller.CommandGetMessages,
		}))
		assert.Equal(t, byte(1), history[2].ToolResult.Contents[1].Image.Data[0])
	})
}

// TestAbortCancelsJoinsAndReportsIdle verifies settlement precedes abort completion.
func (s *ServiceSuite) TestAbortCancelsJoinsAndReportsIdle() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
		release := make(chan struct{})
		var runContextErr error

		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				select {
				case <-ctx.Done():
				case <-release:
				}
				runContextErr = ctx.Err()
				require.NoError(t, delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"))
				return agent.RunOutcomeAborted, context.Canceled
			},
		)
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
		}))
		synctest.Wait()

		gomock.InOrder(
			sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
				CorrelationID: "active", Type: controller.AgentEventAgentSettled, RunID: "run-active",
			}),
			sender.EXPECT().SendResponse(gomock.Any(), controller.Response{
				CorrelationID: "abort", Kind: controller.ResponseAbortCompleted,
			}),
		)
		err := service.Handle(t.Context(), controller.Command{CorrelationID: "abort", Kind: controller.CommandAbort})
		close(release)
		synctest.Wait()

		require.NoError(t, err)
		require.ErrorIs(t, runContextErr, context.Canceled)
		sender.EXPECT().SendResponse(gomock.Any(), controller.Response{
			CorrelationID: "state", Kind: controller.ResponseRunState,
			State: controller.RunStateResult{State: controller.RunStateIdle},
		})
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "state", Kind: controller.CommandGetRunState,
		}))
	})
}

// TestAbortPreservesJoinedNonCancellationError verifies cleanup failures prevent false success.
func (s *ServiceSuite) TestAbortPreservesJoinedNonCancellationError() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
		cleanupErr := errors.New("settlement delivery failed")

		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				<-ctx.Done()
				require.NoError(t, delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"))
				return agent.RunOutcomeAborted, errors.Join(context.Canceled, cleanupErr)
			},
		)
		sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
			CorrelationID: "active", Type: controller.AgentEventAgentSettled, RunID: "run-active",
		})
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
		}))
		synctest.Wait()

		err := service.Handle(t.Context(), controller.Command{CorrelationID: "abort", Kind: controller.CommandAbort})

		require.ErrorIs(t, err, cleanupErr)
		require.NotErrorIs(t, err, context.Canceled)
	})
}

// TestDisconnectCancelsAndJoinsActiveRun verifies controller ownership of active work.
func (s *ServiceSuite) TestDisconnectCancelsAndJoinsActiveRun() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
		release := make(chan struct{})
		var runContextErr error

		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				select {
				case <-ctx.Done():
				case <-release:
				}
				runContextErr = ctx.Err()
				require.NoError(t, delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"))
				return agent.RunOutcomeAborted, context.Canceled
			},
		)
		sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
			CorrelationID: "active", Type: controller.AgentEventAgentSettled, RunID: "run-active",
		})
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
		}))
		synctest.Wait()

		err := service.CancelAndWait(t.Context())
		close(release)
		synctest.Wait()

		require.NoError(t, err)
		require.ErrorIs(t, runContextErr, context.Canceled)
	})
}

// TestDisconnectJoinsRunAfterSettlement verifies idle delivery does not lose the run join handle.
func (s *ServiceSuite) TestDisconnectJoinsRunAfterSettlement() {
	synctest.Test(s.T(), func(t *testing.T) {
		ctrl := gomock.NewController(t)
		coordinator := NewMockCoordinator(ctrl)
		sender := NewMockSender(ctrl)
		delivery := NewDelivery(sender)
		service := New(coordinator, idleStateSnapshot, emptyHistorySnapshot, delivery)
		release := make(chan struct{})
		completed := make(chan error, 1)

		coordinator.EXPECT().PrepareRun().Return("run-active", nil)
		sender.EXPECT().SendResponse(gomock.Any(), gomock.Any())
		sender.EXPECT().SendEvent(gomock.Any(), controller.AgentEvent{
			CorrelationID: "active", Type: controller.AgentEventAgentSettled, RunID: "run-active",
		})
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-active", "first").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				require.NoError(t, delivery.DeliverSettled(context.WithoutCancel(ctx), "run-active"))
				<-release
				return agent.RunOutcomeCompleted, nil
			},
		)
		require.NoError(t, service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: "first",
		}))
		synctest.Wait()

		go func() {
			completed <- service.CancelAndWait(t.Context())
		}()
		synctest.Wait()
		select {
		case err := <-completed:
			require.NoError(t, err)
			assert.Fail(t, "disconnect returned before the settled run completed")
		default:
		}
		close(release)
		synctest.Wait()
		select {
		case err := <-completed:
			require.NoError(t, err)
		default:
			assert.Fail(t, "disconnect did not join the settled run")
		}
	})
}

// TestEmptyCorrelationReturnsTerminalError verifies uncorrelated commands produce no response.
func (s *ServiceSuite) TestEmptyCorrelationReturnsTerminalError() {
	ctrl := gomock.NewController(s.T())
	service := New(
		NewMockCoordinator(ctrl), idleStateSnapshot, emptyHistorySnapshot, NewDelivery(NewMockSender(ctrl)),
	)

	err := service.Handle(s.T().Context(), controller.Command{Kind: controller.CommandGetRunState})

	s.Require().ErrorIs(err, ErrCorrelationRequired)
}

func idleStateSnapshot() run.State {
	return run.State{Status: run.StatusIdle}
}

func emptyHistorySnapshot() []agent.HistoryEntry {
	return nil
}
