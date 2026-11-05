//go:build !integration

package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestAbortRunKeepsIndependentFailure checks error precedence and source completeness under both cancellation orders.
func TestAbortRunKeepsIndependentFailure(t *testing.T) {
	t.Parallel()
	for _, boundary := range []agent.EventType{agent.EventAgentStart, agent.EventTurnEnd} {
		for _, order := range []string{"extension-only", "client-first", "extension-first"} {
			t.Run(fmt.Sprintf("%d/%s", boundary, order), func(t *testing.T) {
				t.Parallel()
				// Arrange an observer that accepts abort and acquires an independent original failure at a live boundary.
				owner, cancel := context.WithCancel(t.Context())
				defer cancel()
				var service *Service
				var histories [][]agent.HistoryEntry
				cause := errors.New(strings.Repeat("independent original cause λ\n", 512) + "complete suffix")
				abort := fmtAbortCause()
				events := NewMockEventSink(gomock.NewController(t))
				events.EXPECT().
					Deliver(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, event agent.Event) error {
						if event.Type == boundary {
							acceptAbortOrder(t, service, cancel, order, "run", abort)
							return cause
						}
						if event.Type == agent.EventAgentEnd {
							require.Equal(t, agent.RunOutcomeFailed, event.Agent.OrEmpty().Outcome)
							require.Contains(t, event.Agent.OrEmpty().ErrorMessage.OrEmpty(), cause.Error())
							require.Contains(t, event.Agent.OrEmpty().ErrorMessage.OrEmpty(), abort.Error())
						}
						return nil
					}).
					AnyTimes()
				service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
					newStopProvider(t, &histories), newNoTools(t), events)
				// Act through the run whose observer acquires both the abort source and the independent failure.
				result, err := service.Run(
					owner,
					runcontrol.Request{RunID: "run", Message: model.TextMessage("request")},
				)
				// Assert failure precedence, complete original text and settlement ownership under every ordering.
				require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
				require.ErrorIs(t, err, cause)
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorContains(t, err, cause.Error())
				require.ErrorContains(t, err, abort.Error())
				require.NoError(t, service.Settle("run"))
			})
		}
	}
}
