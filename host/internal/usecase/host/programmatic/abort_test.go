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
)

// TestAbortCancelsAcceptedOperationWithoutStarting verifies accepted work can be released before Start.
func (s *ServiceSuite) TestAbortCancelsAcceptedOperationWithoutStarting() {
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	coordinator.EXPECT().PrepareRun().Return("run-accepted", nil)
	_, operation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID:   "active",
		Kind:            controller.CommandUserRequest,
		UserText:        mo.Some("request"),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[session.ID](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     controller.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	})
	s.Require().NoError(err)

	response, returnedOperation, err := service.Handle(
		s.T().Context(),
		testProgrammaticCommand("abort", controller.CommandAbort),
	)

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
			CorrelationID:   "active",
			Kind:            controller.CommandUserRequest,
			UserText:        mo.Some("first"),
			ProviderID:      mo.None[model.ProviderID](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[session.ID](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     controller.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
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
			response, _, abortErr := service.Handle(
				t.Context(),
				testProgrammaticCommand("abort", controller.CommandAbort),
			)
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

		state, _, stateErr := service.Handle(
			t.Context(),
			testProgrammaticCommand("state", controller.CommandGetRunState),
		)
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
			CorrelationID:   "active",
			Kind:            controller.CommandUserRequest,
			UserText:        mo.Some("first"),
			ProviderID:      mo.None[model.ProviderID](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[session.ID](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     controller.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		})
		require.NoError(t, err)
		operation.Start()
		synctest.Wait()
		aborted := make(chan error)
		go func() {
			_, _, abortErr := service.Handle(t.Context(), testProgrammaticCommand("abort", controller.CommandAbort))
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
