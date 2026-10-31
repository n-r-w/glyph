//go:build !integration

package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestRunCancellationBeforeDeliveryReturnsPendingMessages checks that a run whose context is canceled after its
// tools or its final response, before the delivery point, ends as aborted, returns the pending client messages, and
// leaves none of them in history.
func TestRunCancellationBeforeDeliveryReturnsPendingMessages(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		// newSetup returns a setup whose first turn ends normally while it cancels the run context.
		newSetup func(t *testing.T, cancel context.CancelFunc) returnedSetup
	}{
		"after a tool batch": {
			newSetup: func(t *testing.T, cancel context.CancelFunc) returnedSetup {
				t.Helper()
				setup := newReturnedSetup(t, toolUseProvider())
				tools := NewMockToolRuntime(gomock.NewController(t))
				tools.EXPECT().Tools().Return(nil).AnyTimes()
				tools.EXPECT().Execute(gomock.Any(), returnedToolCall, gomock.Any()).DoAndReturn(
					func(context.Context, model.ToolCall, tool.ProgressHandler) (agent.ToolResult, error) {
						cancel()
						return agent.ToolResult{
							CallID: returnedToolCall.ID, ToolName: returnedToolCall.Name,
							Contents: tool.TextContents("ok"), IsError: false,
						}, nil
					},
				)
				setup.tools = tools
				return setup
			},
		},
		"after a final response": {
			newSetup: func(t *testing.T, cancel context.CancelFunc) returnedSetup {
				t.Helper()
				return newReturnedSetup(t, func(_ context.Context, _ context.CancelFunc, handle StreamHandler) error {
					cancel()
					return emitStream(handle, emptyModelResponse(model.OutcomeStop), nil)
				})
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a run with pending client messages whose context is canceled at the end of the first turn.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			harness := newReturnedHarness(t, cancel, test.newSetup(t, cancel))

			// Act by running the request.
			result, err := harness.service.Run(
				ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
			)

			// Assert the run was aborted, the client gets every message back, and history holds no client message.
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
			require.Equal(t, agent.RunOutcomeAborted, harness.summary.Outcome)
			requireReturnedMessages(t, harness.summary, 0)
			require.Equal(t, []string{"request"}, userTexts(harness.service.History()))
		})
	}
}
