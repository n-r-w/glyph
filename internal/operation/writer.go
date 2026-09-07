package operation

import (
	"context"
	"errors"
	"sync"
)

// Acknowledgement resolves when one queued message is delivered or delivery stops.
type Acknowledgement struct {
	// done closes after the immutable wait result and optional confirmation link are stored.
	done chan struct{}
	// once protects acknowledgement resolution.
	once sync.Once
	// err is immutable after done closes and survives every waiter.
	err error
	// confirmation is the actual send result when delivery returned before that send completed.
	confirmation *Acknowledgement
}

// newAcknowledgement constructs an unresolved delivery acknowledgement.
func newAcknowledgement() *Acknowledgement {
	return &Acknowledgement{
		done:         make(chan struct{}),
		once:         sync.Once{},
		err:          nil,
		confirmation: nil,
	}
}

// resolve completes a delivery acknowledgement exactly once.
func (a *Acknowledgement) resolve(err error) {
	a.resolveWithConfirmation(err, nil)
}

// resolveWithConfirmation publishes a wait result and the actual result for this message only.
func (a *Acknowledgement) resolveWithConfirmation(err error, confirmation *Acknowledgement) {
	a.once.Do(func() {
		a.err = err
		a.confirmation = confirmation
		close(a.done)
	})
}

// Wait waits for message delivery.
func (a *Acknowledgement) Wait(ctx context.Context) error {
	select {
	case <-a.done:
		return a.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// Result reports final transport completion independently of any canceled acknowledgement wait.
func (a *Acknowledgement) Result() (bool, error) {
	select {
	case <-a.done:
		if a.confirmation != nil {
			return a.confirmation.Result()
		}
		return true, a.err
	default:
		return false, nil
	}
}

// writerItem contains one mapped message and its optional acknowledgement.
type writerItem[M any] struct {
	// message is ready for transport delivery.
	message M
	// acknowledgement is resolved after delivery when present.
	acknowledgement *Acknowledgement
	// source is the declared error reported outside an accepted operation outcome.
	source error
}

// writerReport retains an explicit source until its actual transport result can be inspected.
type writerReport struct {
	// source is the declared diagnostic that was not confirmed when Writer stopped.
	source error
	// confirmation is present only when an actual Send can still finish after its wait was canceled.
	confirmation *Acknowledgement
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
	// sources retains error reports whose enqueue or transport delivery did not succeed.
	sources []writerReport
	// pendingSend retains actual completion when a canceled wait ends Run before Send returns.
	pendingSend *Acknowledgement
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
		send:        send,
		queue:       make(chan writerItem[M], capacity),
		mutex:       sync.Mutex{},
		closed:      false,
		cause:       nil,
		sources:     nil,
		pendingSend: nil,
	}
}

// Enqueue queues one message without a delivery acknowledgement.
func (w *Writer[M]) Enqueue(message M, source error) error {
	return w.enqueue(writerItem[M]{message: message, acknowledgement: nil, source: source})
}

// EnqueueAcknowledged queues one message and returns its delivery acknowledgement.
func (w *Writer[M]) EnqueueAcknowledged(message M, source error) (*Acknowledgement, error) {
	ack := newAcknowledgement()
	if err := w.enqueue(writerItem[M]{message: message, acknowledgement: ack, source: source}); err != nil {
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
		w.sources = append(w.sources, writerReport{source: item.source, confirmation: nil})
		if w.cause != nil {
			return w.cause
		}
		return ErrClosed
	}
	select {
	case w.queue <- item:
		return nil
	default:
		w.sources = append(w.sources, writerReport{source: item.source, confirmation: nil})
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
				w.retainSource(item.source, nil)
				if item.acknowledgement != nil {
					item.acknowledgement.resolve(err)
				}
				w.stop(err)
				w.resolveQueued(err)
				return err
			}
			if err := w.send(item.message); err != nil {
				var confirmation *Acknowledgement
				if pending, ok := errors.AsType[*pendingSendError](err); ok {
					confirmation = pending.confirmation
				}
				w.retainSource(item.source, confirmation)
				if item.acknowledgement != nil {
					item.acknowledgement.resolveWithConfirmation(err, confirmation)
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
		w.retainSource(item.source, nil)
		if item.acknowledgement != nil {
			item.acknowledgement.resolve(err)
		}
	}
}

// retainSource keeps the declared report and actual send confirmation available for separate collection.
func (w *Writer[M]) retainSource(source error, confirmation *Acknowledgement) {
	if source == nil && confirmation == nil {
		return
	}
	w.mutex.Lock()
	if confirmation != nil {
		w.pendingSend = confirmation
	}
	if source != nil {
		w.sources = append(w.sources, writerReport{source: source, confirmation: confirmation})
	}
	w.mutex.Unlock()
}

// PendingSendError returns an available actual Send failure without waiting or including report sources.
func (w *Writer[M]) PendingSendError() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.pendingSend != nil {
		if complete, err := w.pendingSend.Result(); complete {
			return err
		}
	}
	return nil
}

// SourceErrors returns undelivered report causes after producers and writer completion have joined.
func (w *Writer[M]) SourceErrors() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	sources := make([]error, 0, len(w.sources))
	for _, report := range w.sources {
		if report.confirmation != nil {
			if complete, err := report.confirmation.Result(); complete && err == nil {
				continue
			}
		}
		sources = append(sources, report.source)
	}
	return errors.Join(sources...)
}

// Close stops new enqueue operations and drains queued messages.
func (w *Writer[M]) Close() {
	w.stop(nil)
}
