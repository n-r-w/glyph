//go:build !integration

package run

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// TestAbortRunIdentity checks cancellation ordering, completion races and rejected-source isolation between runs.
func TestAbortRunIdentity(t *testing.T) {
	t.Parallel()
	for _, boundary := range []agent.EventType{agent.EventAgentStart, agent.EventTurnEnd} {
		for _, order := range []string{"extension-only", "client-first", "extension-first"} {
			t.Run(fmt.Sprintf("%d/%s", boundary, order), func(t *testing.T) {
				t.Parallel()
				// Arrange two accepted extension aborts at a live boundary and a distinct rejected source.
				owner, cancel := context.WithCancel(t.Context())
				defer cancel()
				var service *Service
				var histories [][]agent.HistoryEntry
				cause := fmtAbortCause()
				later := diagnostic.NewCurrentCause(context.Canceled,
					fmt.Errorf("second accepted extension exact\nsource: %w", context.Canceled))
				rejected := fmt.Errorf("unaccepted extension source: %w", context.Canceled)
				events := NewMockEventSink(gomock.NewController(t))
				events.EXPECT().
					Deliver(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, event agent.Event) error {
						if event.RunID == "first" && event.Type == boundary {
							require.ErrorIs(t, service.Abort("stale", rejected), agent.ErrRunNotActive)
							acceptAbortOrder(t, service, cancel, order, "first", cause)
							require.NoError(t, service.Abort("first", later))
						}
						if event.RunID == "second" && event.Type == agent.EventAgentStart {
							require.ErrorIs(t, service.Abort("first", rejected), agent.ErrRunNotActive)
						}
						if event.Type == agent.EventAgentEnd {
							summary := event.Agent.OrEmpty()
							if event.RunID == "first" {
								require.Equal(t, agent.RunOutcomeAborted, summary.Outcome)
								require.Contains(t, summary.ErrorMessage.OrEmpty(), cause.Error())
								require.Contains(t, summary.ErrorMessage.OrEmpty(), later.Error())
								require.NotContains(t, summary.ErrorMessage.OrEmpty(), "unaccepted extension")
							} else {
								require.True(t, summary.ErrorMessage.IsNone())
							}
						}
						return nil
					}).
					AnyTimes()
				service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
					newStopProvider(t, &histories), newNoTools(t), events)
				require.ErrorIs(t, service.Abort("first", rejected), agent.ErrRunNotActive)

				// Act through the run API and settle before a replacement run with a fresh owning context.
				result, err := service.Run(
					owner,
					runcontrol.Request{RunID: "first", Message: model.TextMessage("first")},
				)

				// Assert complete accepted sources survive either cancellation order, while rejected work never leaks.
				require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorContains(t, err, cause.Error())
				require.ErrorContains(t, err, later.Error())
				require.NotContains(t, err.Error(), "unaccepted extension")
				require.ErrorIs(t, service.Abort("first", rejected), agent.ErrRunNotActive)
				require.NoError(t, service.Settle("first"))
				result, err = service.Run(
					t.Context(),
					runcontrol.Request{RunID: "second", Message: model.TextMessage("second")},
				)
				require.NoError(t, err)
				require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
				require.NoError(t, service.Settle("second"))
			})
		}
	}
}

// acceptAbortOrder drives cancellation and successful acceptance in the specified deterministic order.
func acceptAbortOrder(t *testing.T, service *Service, cancel context.CancelFunc, order, runID string, cause error) {
	t.Helper()
	if order == "client-first" {
		cancel()
	}
	require.NoError(t, service.Abort(runID, cause))
	if order == "extension-first" {
		cancel()
	}
}

// fmtAbortCause supplies an identified cancellation with its complete original cause.
func fmtAbortCause() error {
	return diagnostic.NewCurrentCause(context.Canceled, fmt.Errorf("extension origin exact: %w", context.Canceled))
}
