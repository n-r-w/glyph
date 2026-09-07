package operation

import "context"

// pendingSendError separates a canceled wait from the actual transport completion.
type pendingSendError struct {
	// cause stopped the caller's wait but did not complete the transport send.
	cause error
	// confirmation remains observable when the actual send completes after cancellation.
	confirmation *Acknowledgement
}

// Error preserves the wait cancellation text.
func (e *pendingSendError) Error() string { return e.cause.Error() }

// Unwrap preserves the wait cancellation cause without changing delivery classification.
func (e *pendingSendError) Unwrap() error { return e.cause }

// SendWithContext bounds waiting without losing the actual result needed by Writer's source collection.
// A canceled wait does not stop the send or confirm its failure.
func SendWithContext(ctx context.Context, send func() error) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	confirmation := newAcknowledgement()
	go func() { confirmation.resolve(send()) }()
	select {
	case <-confirmation.done:
		return confirmation.err
	case <-ctx.Done():
		return &pendingSendError{cause: context.Cause(ctx), confirmation: confirmation}
	}
}
