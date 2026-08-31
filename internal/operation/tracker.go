package operation

import (
	"errors"
	"fmt"
	"sync"
)

// EventKind identifies an incoming operation event.
type EventKind uint8

const (
	// EventAccepted reports admission.
	EventAccepted EventKind = iota + 1
	// EventRunning reports work start.
	EventRunning
	// EventProgress carries contract-owned progress.
	EventProgress
	// EventCompleted carries a contract-owned result.
	EventCompleted
	// EventCanceled reports cancellation.
	EventCanceled
	// EventFailed reports failure.
	EventFailed
	// EventRejected reports that no operation was created.
	EventRejected
)

// Event contains one typed incoming lifecycle event.
type Event[P, R any] struct {
	// ID identifies the operation.
	ID string
	// Kind identifies the lifecycle event.
	Kind EventKind
	// Progress contains progress for EventProgress.
	Progress P
	// Result contains a result for EventCompleted.
	Result R
	// Code contains a machine code for EventFailed or EventRejected.
	Code string
}

// trackedState identifies the latest valid nonterminal event.
type trackedState uint8

const (
	// trackedPending waits for Accepted or Rejected.
	trackedPending trackedState = iota
	// trackedAccepted waits for Running.
	trackedAccepted
	// trackedRunning accepts progress or one terminal event.
	trackedRunning
)

// trackedOperation stores validation state and its bounded consumer queue.
type trackedOperation[P, R any] struct {
	// state is the latest valid nonterminal lifecycle state.
	state trackedState
	// events delivers validated events to the operation consumer.
	events chan Event[P, R]
}

// inboundQueueCapacity bounds lifecycle events awaiting one operation consumer.
const inboundQueueCapacity = 64

// Tracker validates events for operations initiated on a connection.
type Tracker[P, R any] struct {
	// capacity bounds each initiated operation's inbound queue.
	capacity int
	// mutex protects operation ownership and lifecycle state.
	mutex sync.Mutex
	// operations contains identifiers awaiting a terminal event or rejection.
	operations map[string]*trackedOperation[P, R]
	// closed reports that the connection no longer accepts events.
	closed bool
}

// NewTracker constructs a tracker with bounded per-operation queues.
func NewTracker[P, R any]() *Tracker[P, R] {
	return newTracker[P, R](inboundQueueCapacity)
}

// newTracker constructs a tracker with a testable internal queue capacity.
func newTracker[P, R any](capacity int) *Tracker[P, R] {
	if capacity <= 0 {
		panic("tracker capacity must be positive")
	}
	return &Tracker[P, R]{
		capacity:   capacity,
		mutex:      sync.Mutex{},
		operations: make(map[string]*trackedOperation[P, R]),
		closed:     false,
	}
}

// Track reserves an identifier before its request is sent.
func (t *Tracker[P, R]) Track(id string) (<-chan Event[P, R], error) {
	if id == "" {
		return nil, errors.New("track operation: empty identifier")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.closed {
		return nil, ErrClosed
	}
	if _, exists := t.operations[id]; exists {
		return nil, ErrIdentifierInUse
	}

	events := make(chan Event[P, R], t.capacity)
	t.operations[id] = &trackedOperation[P, R]{state: trackedPending, events: events}
	return events, nil
}

// Handle validates and enqueues one incoming event.
func (t *Tracker[P, R]) Handle(event Event[P, R]) error {
	if event.ID == "" {
		return errors.New("handle operation event: empty identifier")
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.closed {
		return ErrClosed
	}
	tracked, exists := t.operations[event.ID]
	if !exists {
		return fmt.Errorf("handle operation event %q: identifier is not tracked", event.ID)
	}

	next, terminal, err := validateEvent(tracked.state, event.Kind, event.Code)
	if err != nil {
		return fmt.Errorf("handle operation event %q: %w", event.ID, err)
	}
	select {
	case tracked.events <- event:
		tracked.state = next
		if terminal {
			delete(t.operations, event.ID)
			close(tracked.events)
		}
		return nil
	default:
		return ErrQueueFull
	}
}

// validateEvent checks one lifecycle transition.
func validateEvent(state trackedState, kind EventKind, code string) (trackedState, bool, error) {
	switch state {
	case trackedPending:
		switch kind {
		case EventAccepted:
			return trackedAccepted, false, nil
		case EventRejected:
			if code == "" {
				return state, false, errors.New("rejected event requires a code")
			}
			return state, true, nil
		case EventRunning, EventProgress, EventCompleted, EventCanceled, EventFailed:
			return state, false, fmt.Errorf("event %d cannot precede Accepted", kind)
		default:
			return state, false, fmt.Errorf("unknown event kind %d", kind)
		}
	case trackedAccepted:
		if kind != EventRunning {
			return state, false, fmt.Errorf("event %d cannot precede Running", kind)
		}
		return trackedRunning, false, nil
	case trackedRunning:
		switch kind {
		case EventProgress:
			return trackedRunning, false, nil
		case EventCompleted, EventCanceled:
			return trackedRunning, true, nil
		case EventFailed:
			if code == "" {
				return state, false, errors.New("failed event requires a code")
			}
			return trackedRunning, true, nil
		case EventAccepted, EventRunning, EventRejected:
			return state, false, fmt.Errorf("event %d is not valid while Running", kind)
		default:
			return state, false, fmt.Errorf("unknown event kind %d", kind)
		}
	default:
		return state, false, fmt.Errorf("unknown tracker state %d", state)
	}
}

// Close stops tracking and closes all inbound queues.
func (t *Tracker[P, R]) Close() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	for id, tracked := range t.operations {
		delete(t.operations, id)
		close(tracked.events)
	}
}
