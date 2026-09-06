//go:build !integration

package events

import (
	"context"
	"errors"
	"testing"

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
	event := emptyCoordinatorEvent(agent.EventAgentStart, "run")
	observer.EXPECT().Observe(t.Context(), event).DoAndReturn(func(context.Context, agent.Event) error {
		order = append(order, "observer")
		return nil
	})
	dispatcher := NewDispatcher(func(context.Context, agent.Event) error {
		order = append(order, "client")
		return nil
	}, func(context.Context, string) error { return nil }, observer)

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
	event := emptyCoordinatorEvent(agent.EventAgentStart, "run")
	observer.EXPECT().Observe(gomock.Any(), event).DoAndReturn(func(context.Context, agent.Event) error {
		close(observerStarted)
		<-releaseObserver
		return nil
	})
	dispatcher := NewDispatcher(func(context.Context, agent.Event) error {
		close(clientDelivered)
		return nil
	}, func(context.Context, string) error { return nil }, observer)
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
	event := emptyCoordinatorEvent(agent.EventAgentStart, "run")
	observer.EXPECT().Observe(t.Context(), event).Return(observerErr)
	dispatcher := NewDispatcher(
		func(context.Context, agent.Event) error { return clientErr },
		func(context.Context, string) error {
			return nil
		},
		observer,
	)

	// Act by delivering one source event.
	err := dispatcher.Deliver(t.Context(), event)

	// Assert client failure did not skip observers and both causes remain discoverable.
	require.ErrorIs(t, err, clientErr)
	require.ErrorIs(t, err, observerErr)
}
