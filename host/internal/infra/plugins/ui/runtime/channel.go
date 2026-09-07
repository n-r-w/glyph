package runtime

import (
	"errors"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	initializationOperationID = "host-initialize"
)

// Send writes one Host connection event or operation progress event.
func (c *Service) sendFrame(frame controllerui.Frame) error {
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
	err = writer.Enqueue(mapped, nil)
	if err != nil && !errors.Is(err, operation.ErrClosed) {
		c.reportDeliveryFailure(err)
	}
	return err
}

// BindProgress associates asynchronous Host progress with one operation reporter.
func (c *Service) BindProgress(reporter operation.Reporter[controllerui.Frame]) func() {
	c.mutex.Lock()
	c.progressReporter = reporter
	c.progressBound = true
	c.mutex.Unlock()
	return func() {
		c.mutex.Lock()
		c.progressReporter = operation.Reporter[controllerui.Frame]{}
		c.progressBound = false
		c.mutex.Unlock()
	}
}

// reportDeliveryFailure closes the operation stream after outbound failure.
func (c *Service) reportDeliveryFailure(err error) {
	c.mutex.Lock()
	fail := c.failConnection
	c.mutex.Unlock()
	if fail != nil {
		fail(err)
	}
}

// isOperationProgress reports whether one frame belongs to a running operation.
func isOperationProgress(frame controllerui.Frame) bool {
	if frame.Kind == controllerui.FrameAuthorization || frame.Kind == controllerui.FrameSessionTreeNavigationProgress {
		return true
	}
	if frame.Kind != controllerui.FrameLifecycle {
		return false
	}
	return true
}

// hostEventRequest constructs one Host lifecycle event envelope.
func hostEventRequest(id string, event *uiv1.HostEvent) *uiv1.OpenRequest {
	return uiv1.OpenRequest_builder{
		OperationId: new(id), Request: nil, Event: event,
		ConnectionEvent: nil, Close: nil,
	}.Build()
}
