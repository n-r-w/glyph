//go:build !integration

package operation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTrackerDeliversValidLifecycleInOrder tests the scenario where accepted operations receive ordered events.
func TestTrackerDeliversValidLifecycleInOrder(t *testing.T) {
	t.Parallel()

	// Arrange one tracked operation with room for its complete lifecycle.
	tracker := newTracker[string, string](4)
	events, err := tracker.Track("operation-1")
	require.NoError(t, err)
	lifecycle := []Event[string, string]{
		{ID: "operation-1", Kind: EventAccepted, Progress: "", Result: "", Code: "", Message: ""},
		{ID: "operation-1", Kind: EventRunning, Progress: "", Result: "", Code: "", Message: ""},
		{ID: "operation-1", Kind: EventProgress, Progress: "half", Result: "", Code: "", Message: ""},
		{ID: "operation-1", Kind: EventCompleted, Progress: "", Result: "done", Code: "", Message: ""},
	}

	// Act by handling the valid lifecycle.
	for _, event := range lifecycle {
		require.NoError(t, tracker.Handle(event))
	}

	// Assert the operation queue preserves event order and closes after terminal delivery.
	for _, want := range lifecycle {
		require.Equal(t, want, <-events)
	}
	_, open := <-events
	require.False(t, open)
}

// TestTrackerRejectsInvalidLifecycle tests the scenario where progress cannot precede Running or follow a terminal event.
func TestTrackerRejectsInvalidLifecycle(t *testing.T) {
	t.Parallel()

	// Arrange a newly tracked operation.
	tracker := newTracker[string, string](3)
	_, err := tracker.Track("operation-1")
	require.NoError(t, err)

	// Act by reporting progress before acceptance and running.
	err = tracker.Handle(Event[string, string]{
		ID:       "operation-1",
		Kind:     EventProgress,
		Progress: "early",
		Result:   "",
		Code:     "",
		Message:  "",
	})

	// Assert strict lifecycle validation fails.
	require.Error(t, err)
}

// TestTrackerRejectsUnknownIdentifier tests the scenario where a peer cannot report events for unowned identifiers.
func TestTrackerRejectsUnknownIdentifier(t *testing.T) {
	t.Parallel()

	// Arrange an empty tracker.
	tracker := newTracker[string, string](1)

	// Act by handling an event for an unknown operation.
	err := tracker.Handle(Event[string, string]{
		ID:       "unknown",
		Kind:     EventAccepted,
		Progress: "",
		Result:   "",
		Code:     "",
		Message:  "",
	})

	// Assert identifier ownership validation fails.
	require.Error(t, err)
}

// TestTrackerReturnsQueueFullWithoutBlocking tests the scenario where slow consumers fail the connection path.
func TestTrackerReturnsQueueFullWithoutBlocking(t *testing.T) {
	t.Parallel()

	// Arrange a tracked operation with one inbound queue slot.
	tracker := newTracker[string, string](1)
	_, err := tracker.Track("operation-1")
	require.NoError(t, err)
	require.NoError(t, tracker.Handle(Event[string, string]{
		ID:       "operation-1",
		Kind:     EventAccepted,
		Progress: "",
		Result:   "",
		Code:     "",
		Message:  "",
	}))

	// Act by delivering another event without consuming the first.
	err = tracker.Handle(Event[string, string]{
		ID:       "operation-1",
		Kind:     EventRunning,
		Progress: "",
		Result:   "",
		Code:     "",
		Message:  "",
	})

	// Assert queue exhaustion is reported without waiting for receipt.
	require.ErrorIs(t, err, ErrQueueFull)
}

// TestTrackerAcceptsRejectedOnlyBeforeAcceptance tests the scenario where rejection terminates an unaccepted request.
func TestTrackerAcceptsRejectedOnlyBeforeAcceptance(t *testing.T) {
	t.Parallel()

	// Arrange one tracked request.
	tracker := newTracker[string, string](1)
	events, err := tracker.Track("operation-1")
	require.NoError(t, err)
	rejected := Event[string, string]{
		ID:       "operation-1",
		Kind:     EventRejected,
		Progress: "",
		Result:   "",
		Code:     "BUSY",
		Message:  "another operation is active",
	}

	// Act by reporting admission rejection.
	err = tracker.Handle(rejected)

	// Assert rejection is delivered once and closes the operation queue.
	require.NoError(t, err)
	require.Equal(t, rejected, <-events)
	_, open := <-events
	require.False(t, open)
}

// TestTrackerRejectsLifecycleErrorsWithoutCompleteText tests required public error fields.
func TestTrackerRejectsLifecycleErrorsWithoutCompleteText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		event    Event[string, string]
		required string
	}{
		{
			name: "rejected code",
			event: Event[string, string]{
				ID: "operation-1", Kind: EventRejected, Progress: "", Result: "", Code: "", Message: "busy",
			},
			required: "requires a code",
		},
		{
			name: "rejected message",
			event: Event[string, string]{
				ID: "operation-1", Kind: EventRejected, Progress: "", Result: "", Code: "BUSY", Message: "",
			},
			required: "requires a message",
		},
		{
			name: "failed code",
			event: Event[string, string]{
				ID: "operation-1", Kind: EventFailed, Progress: "", Result: "", Code: "", Message: "failed",
			},
			required: "requires a code",
		},
		{
			name: "failed message",
			event: Event[string, string]{
				ID: "operation-1", Kind: EventFailed, Progress: "", Result: "", Code: "INTERNAL", Message: "",
			},
			required: "requires a message",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange the lifecycle state required by the tested terminal event.
			tracker := NewTracker[string, string]()
			_, err := tracker.Track(test.event.ID)
			require.NoError(t, err)
			if test.event.Kind == EventFailed {
				require.NoError(t, tracker.Handle(Event[string, string]{
					ID: test.event.ID, Kind: EventAccepted, Progress: "", Result: "", Code: "", Message: "",
				}))
				require.NoError(t, tracker.Handle(Event[string, string]{
					ID: test.event.ID, Kind: EventRunning, Progress: "", Result: "", Code: "", Message: "",
				}))
			}

			// Act by handling a public error without complete text.
			err = tracker.Handle(test.event)

			// Assert transport-neutral lifecycle validation rejects the event.
			require.ErrorContains(t, err, test.required)
		})
	}
}
