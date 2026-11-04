//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestExtensionDeliveryFailureReturnsOnlyClientMessages checks popped-message cleanup after delayed commit fails.
func TestExtensionDeliveryFailureReturnsOnlyClientMessages(t *testing.T) {
	t.Parallel()
	// Arrange all mode with an extension before a client at the same delivery point.
	var histories [][]agent.HistoryEntry
	var service *Service
	var summary agent.RunSummary
	message := session.QueuedExtensionMessage{
		Session: session.Identity{
			ID:               "session",
			WorkingDirectory: "/project",
			Incarnation:      1,
		},
		RuntimeInstanceID: "runtime",
		Message: session.ExtensionMessage{
			ExtensionID: "extension",
			EntryType:   "kind",
			Text:        "extension",
			Visibility:  session.ClientVisibilityVisible,
		},
	}
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventTurnStart {
			require.NoError(t, service.EnqueueExtension("run", agent.MessageDeliverySteer, message))
			require.NoError(t, service.Enqueue("run", agent.MessageDeliverySteer, queuedText("client original")))
		}
		if event.Type == agent.EventAgentEnd {
			summary = event.Agent.MustGet()
		}
		return nil
	}).AnyTimes()
	service = newTestService(t, testInstructions, testModelDescriptor, model.ReasoningChoiceOff,
		newStopProvider(t, &histories), newNoTools(t), events)
	service.SetQueueMode(agent.QueueModeAll)
	delivery := NewMockExtensionMessageDelivery(gomock.NewController(t))
	cause := errors.New("queued append failed: disk cause")
	delivery.EXPECT().DeliverExtensionMessage(gomock.Any(), message).Return(cause)
	service.BindExtensionMessages(delivery)

	// Act by running through a delayed commit failure with a popped client message behind it.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("request")})

	// Assert only the client's original payload is returned and extension work is discarded.
	require.ErrorIs(t, err, cause)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.Equal(
		t,
		[]agent.ReturnedMessage{{Delivery: agent.MessageDeliverySteer, Message: model.TextMessage("client original")}},
		summary.ReturnedMessages,
	)
	require.NoError(t, service.Settle("run"))
	require.ErrorIs(t, service.EnqueueExtension("run", agent.MessageDeliverySteer, message), agent.ErrRunNotActive)
}
