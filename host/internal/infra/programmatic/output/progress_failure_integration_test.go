//go:build integration

package output

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"

	host "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
)

// TestRunPreparedProgressFailureStopsTerminalDeliveryBeforeJoin verifies failed progress cannot deadlock owner joining.
func TestRunPreparedProgressFailureStopsTerminalDeliveryBeforeJoin(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange a real Agent Core run and an owner delivery that maps progress to a failed connection.
		mockController := gomock.NewController(t)
		delivery := New()
		historyStore := agentrun.NewMockHistoryStore(mockController)
		history := make([]agent.HistoryEntry, 0, 1)
		historyStore.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
			return append([]agent.HistoryEntry(nil), history...)
		}).AnyTimes()
		historyStore.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				history = append(history, entry.Clone())
				return nil
			},
		).AnyTimes()
		eventSink := agentrun.NewMockEventSink(mockController)
		eventSink.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(delivery.DeliverAgent).AnyTimes()
		provider := agentrun.NewMockModelProvider(mockController)
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ agentrun.ModelRequest, _ agentrun.StreamHandler) error {
				<-ctx.Done()
				return context.Cause(ctx)
			},
		).AnyTimes()
		runtime := agentrun.NewMockEffectiveContextPort(mockController)
		runtime.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, request agentrun.ContextRequest) (agentrun.EffectiveContext, error) {
				return agentrun.EffectiveContext{Request: agentrun.ModelRequest{
					Instructions: request.Instructions, Model: model.Descriptor{
						Provider:              "provider",
						Model:                 "model",
						Input:                 nil,
						ContextWindow:         0,
						MaxTokens:             0,
						ReasoningCapabilities: model.ReasoningCapabilities{},
						ToolCapabilities:      model.ToolCapabilities{},
						Pricing:               mo.None[model.Pricing](),
					}, ReasoningChoice: model.ReasoningChoiceHigh,
					History: request.History, Tools: request.Tools,
				}, Provider: provider}, nil
			}).AnyTimes()
		tools := agentrun.NewMockToolRuntime(mockController)
		tools.EXPECT().Tools().Return(nil).AnyTimes()
		agentCore := agentrun.New(
			"instructions", runtime, tools, eventSink, historyStore, passThroughFinalizer(mockController),
		)
		coordinator := host.NewMockCoordinator(mockController)
		coordinator.EXPECT().PrepareRun().Return("run-progress-failure", nil)
		coordinator.EXPECT().
			RunPrepared(gomock.Any(), "run-progress-failure", model.TextMessage("request")).
			DoAndReturn(
				func(ctx context.Context, runID string, message model.Message) (agent.RunOutcome, error) {
					result, err := agentCore.Run(ctx, runcontrol.Request{RunID: runID, Message: message})
					return result.Outcome, err
				},
			)
		service := host.New(coordinator, nil, agentCore, nil, nil, nil, delivery, nil, nil)
		command := controller.Command{
			OperationID:            "progress-failure",
			Kind:                   controller.CommandUserRequest,
			UserMessage:            mo.None[model.Message](),
			ProviderID:             mo.None[model.ProviderID](),
			ModelID:                mo.None[model.ID](),
			ReasoningChoice:        mo.None[model.ReasoningChoice](),
			SessionID:              mo.None[session.ID](),
			SessionName:            mo.None[string](),
			TargetEntryID:          mo.None[string](),
			SummaryMode:            controller.SummaryModeNoSummary,
			CustomFocus:            mo.None[string](),
			EntryLabel:             mo.None[string](),
			RetryEnabled:           mo.None[bool](),
			CompactionInstructions: mo.None[string](),
		}
		command.UserMessage = mo.Some(model.TextMessage("request"))
		prepared, err := service.Prepare(t.Context(), command)
		require.NoError(t, err)

		writer, constructionErr := operation.NewWriter(func(string) error { return nil })
		require.NoError(t, constructionErr)
		writerResult := make(chan error, 1)
		go func() { writerResult <- writer.Run(t.Context()) }()
		connectionErr := status.Error(codes.Unavailable, "send failed")
		ownerDelivery := operationmock.NewMockOperationDelivery[controller.OperationProgress, controller.Response](
			mockController,
		)
		ownerDelivery.EXPECT().Accepted("progress-failure").DoAndReturn(
			func(string) (*operation.Acknowledgement, error) {
				return writer.EnqueueAcknowledged("accepted", nil)
			},
		)
		ownerDelivery.EXPECT().Running("progress-failure").Return(nil)
		ownerDelivery.EXPECT().Progress("progress-failure", gomock.Any()).Return(connectionErr)
		owner, constructionErr := operation.NewOwner(t.Context(), ownerDelivery)
		require.NoError(t, constructionErr)
		require.NoError(t, owner.Start("progress-failure", func() (
			operation.Prepared[controller.OperationProgress, controller.Response], error,
		) {
			return prepared, nil
		}))

		// Act until failed progress cancels Core and the operation worker finishes.
		synctest.Wait()
		owner.Wait()
		writer.Close()
		require.NoError(t, <-writerResult)

		// Assert Core finished without a second progress call or a terminal enqueue.
		assert.Equal(t, agentrun.StatusAwaitingSettlement, agentCore.State().Status)
		require.ErrorIs(t, owner.Err(), connectionErr)
		assert.Equal(t, codes.Unavailable, status.Code(owner.Err()))
	})
}

// passThroughFinalizer returns an Agent Core finalizer whose final values equal the given values.
func passThroughFinalizer(controller *gomock.Controller) *agentrun.MockMessageFinalizer {
	finalizer := agentrun.NewMockMessageFinalizer(controller)
	finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response model.Response) (model.Response, error) { return response, nil },
	).AnyTimes()
	finalizer.EXPECT().FinalizeToolResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, result agent.ToolResult) (agent.ToolResult, error) { return result, nil },
	).AnyTimes()
	return finalizer
}
