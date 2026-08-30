package programmatic

import (
	"context"
	"errors"

	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

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
				deliveryErr := delivery.DeliverAgent(
					context.WithoutCancel(ctx), testEmptyRunEvent(run.EventAgentStart, "run-active"),
				)
				runContextErr = ctx.Err()
				return agent.RunOutcomeAborted, errors.Join(context.Canceled, deliveryErr)
			},
		)
		_, operation, err := service.Handle(t.Context(), controller.Command{
			CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
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
			SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
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
		s.T().Context(), testProgrammaticCommand("", controller.CommandGetRunState),
	)

	s.Require().ErrorIs(err, ErrCorrelationRequired)
	s.Equal(controller.Response{}, response)
	s.Nil(operation)
}
