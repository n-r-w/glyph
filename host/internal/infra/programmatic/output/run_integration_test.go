//go:build integration

package output

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	host "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
)

// runCommand creates a valid user request for operation-ordering scenarios.
func runCommand(id string) controller.Command {
	return controller.Command{
		OperationID: id, Kind: controller.CommandUserRequest, UserText: mo.Some("request"),
		ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.None[session.ID](),
		SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}

// startEvent supplies the payload-free start fact emitted by a coordinated run.
func startEvent(runID string) agent.Event {
	return agent.Event{
		Type:       agent.EventAgentStart,
		RunID:      runID,
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
	}
}

// TestAcceptedOperationStartsExplicitlyAndBackpressures checks acceptance acknowledgement and reporter backpressure.
func TestAcceptedOperationStartsExplicitlyAndBackpressures(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange real prepared work and output with a controlled acknowledgement and progress consumer.
		ctrl := gomock.NewController(t)
		output := New()
		coordinator := host.NewMockCoordinator(ctrl)
		service := host.New(coordinator, nil, nil, nil, nil, nil, output)
		started := make(chan struct{})
		delivered := make(chan struct{})
		accept := make(chan struct{})
		consume := make(chan struct{})
		writer := operation.NewWriter(func(value string) error {
			if value == "accepted" {
				<-accept
			}
			return nil
		})
		writerDone := make(chan error, 1)
		go func() { writerDone <- writer.Run(t.Context()) }()
		coordinator.EXPECT().PrepareRun().Return("run-1", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-1", "request").DoAndReturn(
			func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
				close(started)
				if err := output.DeliverAgent(ctx, startEvent("run-1")); err != nil {
					return agent.RunOutcomeFailed, err
				}
				close(delivered)
				return agent.RunOutcomeCompleted, output.DeliverSettled(ctx, "run-1")
			},
		)
		prepared, err := service.Prepare(t.Context(), runCommand("c1"))
		require.NoError(t, err)
		delivery := operationmock.NewMockOperationDelivery[controller.OperationProgress, controller.Response](ctrl)
		delivery.EXPECT().Accepted("c1").DoAndReturn(func(string) (*operation.Acknowledgement, error) {
			return writer.EnqueueAcknowledged("accepted")
		})
		delivery.EXPECT().Running("c1").Return(nil)
		delivery.EXPECT().
			Progress("c1", gomock.Any()).
			DoAndReturn(func(_ string, progress controller.OperationProgress) error {
				require.Equal(t, "c1", progress.AgentEvent.MustGet().OperationID)
				require.Equal(t, "run-1", progress.AgentEvent.MustGet().RunID)
				<-consume
				return nil
			})
		delivery.EXPECT().
			Terminal("c1", gomock.Any()).
			DoAndReturn(func(_ string, outcome operation.Outcome[controller.Response]) (*operation.Acknowledgement, error) {
				require.Equal(t, operation.TerminalStateCompleted, outcome.State())
				require.Empty(t, output.ActiveOperation())
				return writer.EnqueueAcknowledged("terminal")
			})
		owner := operation.NewOwner(t.Context(), delivery)
		require.NoError(
			t,
			owner.Start("c1", func() (operation.Prepared[controller.OperationProgress, controller.Response], error) {
				return prepared, nil
			}),
		)

		// Act while acceptance is not acknowledged, then acknowledge it without consuming progress.
		synctest.Wait()
		require.Empty(t, started)
		select {
		case <-started:
			t.Fatal("Core started before acceptance acknowledgement")
		default:
		}
		close(accept)
		synctest.Wait()
		select {
		case <-started:
		default:
			t.Fatal("Core did not start after acceptance acknowledgement")
		}
		select {
		case <-delivered:
			t.Fatal("progress producer did not wait for its consumer")
		default:
		}
		close(consume)
		owner.Wait()

		// Assert settlement and prepared cleanup complete before terminal delivery and writer shutdown.
		require.NoError(t, owner.Err())
		select {
		case <-delivered:
		default:
			t.Fatal("progress producer did not finish")
		}
		writer.Close()
		require.NoError(t, <-writerDone)
	})
}

// TestSequentialRunsKeepPreparedRunIDs checks that settlement admits the next correlated run.
func TestSequentialRunsKeepPreparedRunIDs(t *testing.T) {
	t.Parallel()
	// Arrange one Host and its output owner for two sequential preparations.
	ctrl := gomock.NewController(t)
	output := New()
	coordinator := host.NewMockCoordinator(ctrl)
	service := host.New(coordinator, nil, nil, nil, nil, nil, output)
	for _, id := range []string{"first", "second"} {
		coordinator.EXPECT().PrepareRun().Return(id, nil)
		coordinator.EXPECT().
			RunPrepared(gomock.Any(), id, "request").
			DoAndReturn(func(ctx context.Context, runID, _ string) (agent.RunOutcome, error) {
				return agent.RunOutcomeCompleted, output.DeliverSettled(ctx, runID)
			})
		// Act through preparation, execution, settlement, and cleanup.
		prepared, err := service.Prepare(t.Context(), runCommand(id))
		require.NoError(t, err)
		outcome := prepared.Run(t.Context(), operation.Reporter[controller.OperationProgress]{})
		prepared.Release()
		// Assert the operation completes with its own identifier and releases output correlation.
		require.Equal(t, operation.TerminalStateCompleted, outcome.State())
		result, present := outcome.Result()
		require.True(t, present)
		require.Equal(t, id, result.OperationID)
		require.Empty(t, output.ActiveOperation())
	}
}
