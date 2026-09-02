package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	initializationOperationID    = "host-initialize"
	rejectionCodeInvalidArgument = "INVALID_ARGUMENT"
	rejectionCodeBusy            = "BUSY"
	rejectionCodeNotReady        = "NOT_READY"
	rejectionCodeTargetNotActive = "TARGET_NOT_ACTIVE"
	failureCodeInternal          = "INTERNAL"
	failureCodeAuthentication    = "AUTHENTICATION_FAILED"
)

// channel maps provider-neutral Host state onto the UI operation stream.
type channel struct {
	// stream is the generated bidirectional UI stream.
	stream uiv1.UIService_OpenClient
	// cancel stops the stream context.
	cancel context.CancelFunc
	// closed reports whether the channel was closed.
	closed atomic.Bool
	// mutex serializes stream sends and active operation state.
	mutex sync.Mutex
	// ready reports that initialization completed successfully.
	ready bool
	// writer serializes operation-mode Host messages.
	writer *operation.Writer[*uiv1.OpenRequest]
	// progressReporter routes asynchronous Host progress through the operation owner.
	progressReporter operation.Reporter[domainui.Frame]
	// progressBound reports that an operation reporter is active.
	progressBound bool
	// failConnection closes the active operation stream after outbound failure.
	failConnection func(error)
}

var _ hostui.Channel = (*channel)(nil)

// Send writes one Host connection event or operation progress event.
func (c *channel) Send(frame domainui.Frame) error {
	mapped, err := mapFrame(frame)
	if err != nil {
		return err
	}
	c.mutex.Lock()
	writer := c.writer
	reporter := c.progressReporter
	progressBound := c.progressBound
	c.mutex.Unlock()
	if isOperationProgress(frame) {
		if !progressBound {
			return errors.New("send UI progress: operation reporter is not bound")
		}
		return reporter.Report(frame)
	}
	if writer == nil {
		return errors.New("send UI frame: operation writer is not running")
	}
	err = writer.Enqueue(mapped)
	if err != nil && !errors.Is(err, operation.ErrClosed) {
		c.reportDeliveryFailure(err)
	}
	return err
}

// BindProgress associates asynchronous Host progress with one operation reporter.
func (c *channel) BindProgress(reporter operation.Reporter[domainui.Frame]) func() {
	c.mutex.Lock()
	c.progressReporter = reporter
	c.progressBound = true
	c.mutex.Unlock()
	return func() {
		c.mutex.Lock()
		c.progressReporter = operation.Reporter[domainui.Frame]{}
		c.progressBound = false
		c.mutex.Unlock()
	}
}

// reportDeliveryFailure closes the operation stream after outbound failure.
func (c *channel) reportDeliveryFailure(err error) {
	c.mutex.Lock()
	fail := c.failConnection
	c.mutex.Unlock()
	if fail != nil {
		fail(err)
	}
}

// isOperationProgress reports whether one frame belongs to a running operation.
func isOperationProgress(frame domainui.Frame) bool {
	if frame.Kind == domainui.FrameAuthorization {
		return true
	}
	if frame.Kind != domainui.FrameLifecycle {
		return false
	}
	lifecycle, present := frame.Lifecycle.Get()
	return present && lifecycle.Type != domainui.LifecycleAvailabilityChanged
}

// Close cancels the stream context to unblock pending operations.
func (c *channel) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.cancel()
	}
}

// hostEventRequest constructs one Host lifecycle event envelope.
func hostEventRequest(id string, event *uiv1.HostEvent) *uiv1.OpenRequest {
	return uiv1.OpenRequest_builder{
		OperationId: new(id), Request: nil, Event: event,
		ConnectionEvent: nil, Close: nil,
	}.Build()
}
