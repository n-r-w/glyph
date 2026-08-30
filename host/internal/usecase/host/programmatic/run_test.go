package programmatic

import (
	"context"

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
				if err := delivery.DeliverAgent(ctx, testEmptyRunEvent(run.EventAgentStart, "run-1")); err != nil {
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
			SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](),
		})

		require.NoError(t, err)
		assert.Equal(t, controller.Response{
			SessionEntries: nil,
			CorrelationID:  "c1", Kind: controller.ResponseUserRequestAccepted, State: mo.None[controller.RunStateResult](), Messages: nil, Models: mo.None[controller.ModelsResult](), Selection: mo.None[model.Selection](), Rejection: mo.None[controller.Rejection](),
			SessionInfo:       mo.None[session.Info](),
			Sessions:          nil,
			SessionStatistics: mo.None[session.Statistics](), SessionTree: mo.None[controller.SessionTree](), TreeNavigation: mo.None[controller.TreeNavigationResult](),
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

		assert.Equal(
			t,
			testEmptyAgentEvent(controller.AgentEventAgentStart, "c1", "run-1"),
			<-operation.Events(),
		)
		synctest.Wait()
		select {
		case <-delivered:
		default:
			assert.Fail(t, "event producer remained blocked after consumption")
		}
		assert.Equal(
			t,
			testEmptyAgentEvent(controller.AgentEventAgentSettled, "c1", "run-1"),
			<-operation.Events(),
		)
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
				SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](),
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
