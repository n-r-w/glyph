package operation

import (
	"context"
	"sync"
)

// Acknowledgement resolves when one queued message is delivered or delivery stops.
type Acknowledgement struct {
	// done receives the delivery result exactly once.
	done chan error
	// once protects acknowledgement resolution.
	once sync.Once
}

// newAcknowledgement constructs an unresolved delivery acknowledgement.
func newAcknowledgement() *Acknowledgement {
	return &Acknowledgement{
		done: make(chan error, 1),
		once: sync.Once{},
	}
}

// resolve completes a delivery acknowledgement exactly once.
func (a *Acknowledgement) resolve(err error) {
	a.once.Do(func() {
		a.done <- err
		close(a.done)
	})
}

// Wait waits for message delivery.
func (a *Acknowledgement) Wait(ctx context.Context) error {
	select {
	case err := <-a.done:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// writerItem contains one mapped message and its optional acknowledgement.
type writerItem[M any] struct {
	// message is ready for transport delivery.
	message M
	// acknowledgement is resolved after delivery when present.
	acknowledgement *Acknowledgement
}

// outboundQueueCapacity bounds queued messages for one connection.
const outboundQueueCapacity = 64

// Writer sends mapped messages in bounded queue order from one goroutine.
type Writer[M any] struct {
	// send delivers one mapped message through the owning transport.
	send func(M) error
	// queue bounds messages waiting for transport delivery.
	queue chan writerItem[M]
	// mutex protects closure and enqueue against channel-close races.
	mutex sync.Mutex
	// closed reports that no more messages can be enqueued.
	closed bool
	// cause is returned to producers after delivery stops.
	cause error
}

// NewWriter constructs a bounded writer.
func NewWriter[M any](send func(M) error) *Writer[M] {
	return newWriter(outboundQueueCapacity, send)
}

// newWriter constructs a writer with a testable internal capacity.
func newWriter[M any](capacity int, send func(M) error) *Writer[M] {
	if capacity <= 0 {
		panic("writer capacity must be positive")
	}
	if send == nil {
		panic("writer send function is required")
	}

	return &Writer[M]{
		send:   send,
		queue:  make(chan writerItem[M], capacity),
		mutex:  sync.Mutex{},
		closed: false,
		cause:  nil,
	}
}

// Enqueue queues one message without a delivery acknowledgement.
func (w *Writer[M]) Enqueue(message M) error {
	return w.enqueue(writerItem[M]{message: message, acknowledgement: nil})
}

// EnqueueAcknowledged queues one message and returns its delivery acknowledgement.
func (w *Writer[M]) EnqueueAcknowledged(message M) (*Acknowledgement, error) {
	ack := newAcknowledgement()
	if err := w.enqueue(writerItem[M]{message: message, acknowledgement: ack}); err != nil {
		ack.resolve(err)
		return nil, err
	}
	return ack, nil
}

// enqueue adds one item without waiting for queue capacity.
func (w *Writer[M]) enqueue(item writerItem[M]) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closed {
		if w.cause != nil {
			return w.cause
		}
		return ErrClosed
	}
	select {
	case w.queue <- item:
		return nil
	default:
		return ErrQueueFull
	}
}

// Run sends queued messages until closure, context cancellation, or delivery failure.
func (w *Writer[M]) Run(ctx context.Context) error {
	for {
		if err := context.Cause(ctx); err != nil {
			w.stop(err)
			w.resolveQueued(err)
			return err
		}

		select {
		case item, open := <-w.queue:
			if !open {
				return nil
			}
			if err := context.Cause(ctx); err != nil {
				if item.acknowledgement != nil {
					item.acknowledgement.resolve(err)
				}
				w.stop(err)
				w.resolveQueued(err)
				return err
			}
			if err := w.send(item.message); err != nil {
				if item.acknowledgement != nil {
					item.acknowledgement.resolve(err)
				}
				w.stop(err)
				w.resolveQueued(err)
				return err
			}
			if item.acknowledgement != nil {
				item.acknowledgement.resolve(nil)
			}
		case <-ctx.Done():
			err := context.Cause(ctx)
			w.stop(err)
			w.resolveQueued(err)
			return err
		}
	}
}

// stop closes the queue and records why delivery stopped.
func (w *Writer[M]) stop(cause error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.cause = cause
	close(w.queue)
}

// resolveQueued releases acknowledgements for messages that cannot be delivered.
func (w *Writer[M]) resolveQueued(err error) {
	for item := range w.queue {
		if item.acknowledgement != nil {
			item.acknowledgement.resolve(err)
		}
	}
}

// Close stops new enqueue operations and drains queued messages.
func (w *Writer[M]) Close() {
	w.stop(nil)
}
