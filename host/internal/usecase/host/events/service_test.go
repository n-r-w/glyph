//go:build !integration

package events

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestDispatcherAttemptsClientBeforeObservers verifies strict client-first synchronous delivery.
func TestDispatcherAttemptsClientBeforeObservers(t *testing.T) {
	t.Parallel()

	// Arrange one client delivery and one observer that record completion order.
	controller := gomock.NewController(t)
	observer := NewMockObserver(controller)
	order := make([]string, 0, 2)
	event := emptyEvent(agent.EventAgentStart, "run")
	observer.EXPECT().Observe(t.Context(), event).DoAndReturn(func(context.Context, agent.Event) error {
		order = append(order, "observer")
		return nil
	})
	client := NewMockClientDelivery(controller)
	client.EXPECT().DeliverAgent(gomock.Any(), event).DoAndReturn(func(context.Context, agent.Event) error {
		order = append(order, "client")
		return nil
	})
	dispatcher := NewDispatcher(client, observer)

	// Act by delivering one source event.
	err := dispatcher.Deliver(t.Context(), event)

	// Assert client delivery completed before the observer returned.
	require.NoError(t, err)
	assert.Equal(t, []string{"client", "observer"}, order)
}

// TestDispatcherWaitsForObserverBeforeReturning verifies source event processing cannot overtake the observer chain.
func TestDispatcherWaitsForObserverBeforeReturning(t *testing.T) {
	t.Parallel()

	// Arrange one observer blocked on an explicit release after client delivery.
	controller := gomock.NewController(t)
	observer := NewMockObserver(controller)
	clientDelivered := make(chan struct{})
	observerStarted := make(chan struct{})
	releaseObserver := make(chan struct{})
	event := emptyEvent(agent.EventAgentStart, "run")
	observer.EXPECT().Observe(gomock.Any(), event).DoAndReturn(func(context.Context, agent.Event) error {
		close(observerStarted)
		<-releaseObserver
		return nil
	})
	client := NewMockClientDelivery(controller)
	client.EXPECT().DeliverAgent(gomock.Any(), event).DoAndReturn(func(context.Context, agent.Event) error {
		close(clientDelivered)
		return nil
	})
	dispatcher := NewDispatcher(client, observer)
	result := make(chan error, 1)

	// Act by starting delivery while the observer remains blocked.
	go func() { result <- dispatcher.Deliver(t.Context(), event) }()
	<-clientDelivered
	<-observerStarted

	// Assert delivery has not completed until the synchronous observer is released.
	select {
	case err := <-result:
		require.Fail(t, "delivery returned before observer completion", "%v", err)
	default:
	}
	close(releaseObserver)
	require.NoError(t, <-result)
}

// TestDispatcherObservesAfterClientFailureAndReturnsCompleteCauses verifies both attempts and joined diagnostics.
func TestDispatcherObservesAfterClientFailureAndReturnsCompleteCauses(t *testing.T) {
	t.Parallel()

	// Arrange independent client and observer failures.
	controller := gomock.NewController(t)
	observer := NewMockObserver(controller)
	clientErr := errors.New("client exact cause")
	observerErr := errors.New("observer exact cause")
	event := emptyEvent(agent.EventAgentStart, "run")
	observer.EXPECT().Observe(t.Context(), event).Return(observerErr)
	client := NewMockClientDelivery(controller)
	client.EXPECT().DeliverAgent(t.Context(), event).Return(clientErr)
	dispatcher := NewDispatcher(client, observer)

	// Act by delivering one source event.
	err := dispatcher.Deliver(t.Context(), event)

	// Assert client failure did not skip observers and both causes remain discoverable.
	require.ErrorIs(t, err, clientErr)
	require.ErrorIs(t, err, observerErr)
}

// emptyEvent creates an event without a variant payload for dispatcher ordering tests.
func emptyEvent(kind agent.EventType, runID string) agent.Event {
	return agent.Event{
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
		Type:       kind,
		RunID:      runID,
	}
}

// TestDispatcherSettledPreservesBothCauses verifies settlement observers run after failed client delivery.
func TestDispatcherSettledPreservesBothCauses(t *testing.T) {
	t.Parallel()
	// Arrange independent client and observer settlement failures.
	controller := gomock.NewController(t)
	client := NewMockClientDelivery(controller)
	observer := NewMockObserver(controller)
	clientErr := errors.New("client settlement exact cause")
	observerErr := errors.New("observer settlement exact cause")
	gomock.InOrder(
		client.EXPECT().DeliverSettled(t.Context(), "run").Return(clientErr),
		observer.EXPECT().ObserveSettled(t.Context(), "run").Return(observerErr),
	)
	dispatcher := NewDispatcher(client, observer)
	// Act by delivering settlement to both recipients.
	err := dispatcher.DeliverSettled(t.Context(), "run")
	// Assert both complete causes survive the ordered attempts.
	require.ErrorIs(t, err, clientErr)
	require.ErrorIs(t, err, observerErr)
	require.ErrorContains(t, err, clientErr.Error())
	require.ErrorContains(t, err, observerErr.Error())
}
