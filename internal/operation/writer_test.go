//go:build !integration

package operation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriterDeliversAcknowledgementAfterSend tests the scenario where acknowledgements represent completed delivery.
func TestWriterDeliversAcknowledgementAfterSend(t *testing.T) {
	t.Parallel()

	// Arrange a writer whose transport records delivered messages.
	delivered := make(chan string, 1)
	writer := newWriter(1, func(message string) error {
		delivered <- message
		return nil
	})
	runDone := make(chan error, 1)
	go func() { runDone <- writer.Run(t.Context()) }()

	// Act by enqueueing an acknowledged message.
	ack, err := writer.EnqueueAcknowledged("accepted")

	// Assert delivery completes before its acknowledgement.
	require.NoError(t, err)
	require.Equal(t, "accepted", <-delivered)
	require.NoError(t, ack.Wait(t.Context()))
	writer.Close()
	require.NoError(t, <-runDone)
}

// TestWriterReturnsQueueFullWithoutBlocking tests the scenario where bounded enqueue failure is immediate.
func TestWriterReturnsQueueFullWithoutBlocking(t *testing.T) {
	t.Parallel()

	// Arrange a writer that has not started consuming its single queue slot.
	writer := newWriter(1, func(string) error { return nil })
	require.NoError(t, writer.Enqueue("first"))

	// Act by enqueueing another message into the full queue.
	err := writer.Enqueue("second")

	// Assert the bounded queue reports its stable failure.
	require.ErrorIs(t, err, ErrQueueFull)
}

// TestWriterPreCanceledContextSkipsSend tests the scenario where unexpected cleanup stops before dequeue delivery.
func TestWriterPreCanceledContextSkipsSend(t *testing.T) {
	t.Parallel()

	// Arrange a queued acknowledged message and an already canceled writer context.
	var sends int
	writer := newWriter(1, func(string) error {
		sends++
		return nil
	})
	ack, err := writer.EnqueueAcknowledged("queued")
	require.NoError(t, err)
	ctx, cancel := context.WithCancelCause(t.Context())
	cancellationErr := errors.New("connection canceled")
	cancel(cancellationErr)

	// Act by running the writer after cancellation is observable.
	runErr := writer.Run(ctx)

	// Assert no transport send occurs and the queued acknowledgement receives the cause.
	require.ErrorIs(t, runErr, cancellationErr)
	require.Zero(t, sends)
	require.ErrorIs(t, ack.Wait(t.Context()), cancellationErr)
}

// TestWriterResolvesPendingAcknowledgementsOnDeliveryFailure tests the scenario where send failure cannot leak waiters.
func TestWriterResolvesPendingAcknowledgementsOnDeliveryFailure(t *testing.T) {
	t.Parallel()

	// Arrange a writer whose transport fails its first delivery.
	deliveryErr := errors.New("delivery failed")
	writer := newWriter(2, func(string) error { return deliveryErr })
	first, err := writer.EnqueueAcknowledged("first")
	require.NoError(t, err)
	second, err := writer.EnqueueAcknowledged("second")
	require.NoError(t, err)

	// Act by running the writer until transport failure.
	runErr := writer.Run(t.Context())

	// Assert both the active and queued acknowledgements receive the same failure.
	require.ErrorIs(t, runErr, deliveryErr)
	require.ErrorIs(t, first.Wait(t.Context()), deliveryErr)
	require.ErrorIs(t, second.Wait(t.Context()), deliveryErr)
}
