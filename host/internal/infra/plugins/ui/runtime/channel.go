package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// channel maps provider-neutral frames and commands to the generated contract.
type channel struct {
	// stream is the generated bidirectional UI stream.
	stream uipb.UIService_OpenClient
	// cancel stops the stream context.
	cancel context.CancelFunc
	// closed reports whether the channel was closed.
	closed atomic.Bool
	// mutex serializes stream sends.
	mutex sync.Mutex
}

var _ hostui.Channel = (*channel)(nil)

// Send writes one Host frame synchronously through the serialized stream path.
func (c *channel) Send(frame domainui.Frame) error {
	mapped, err := mapFrame(frame)
	if err != nil {
		return err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if sendErr := c.stream.Send(mapped); sendErr != nil {
		streamErr := c.stream.Context().Err()
		if errors.Is(sendErr, context.Canceled) || status.Code(sendErr) == codes.Canceled ||
			errors.Is(sendErr, io.EOF) && errors.Is(streamErr, context.Canceled) {
			return fmt.Errorf("send UI frame: %w", context.Canceled)
		}
		wrappedSendErr := fmt.Errorf("send UI frame: %w", sendErr)
		if streamErr != nil &&
			!errors.Is(sendErr, streamErr) && !errors.Is(streamErr, sendErr) {
			return errors.Join(wrappedSendErr, fmt.Errorf("UI stream context: %w", streamErr))
		}
		return wrappedSendErr
	}
	return nil
}

// Receive blocks until the UI sends one command or the stream terminates.
func (c *channel) Receive() (domainui.Command, error) {
	command, err := c.stream.Recv()
	if err != nil {
		return domainui.Command{}, fmt.Errorf("receive UI command: %w", err)
	}
	return mapCommand(command)
}

// Close cancels the stream context to unblock pending send and receive calls.
func (c *channel) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.cancel()
	}
}
