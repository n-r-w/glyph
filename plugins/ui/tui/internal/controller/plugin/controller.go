// Package plugin validates public SDK input before application dispatch.
package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// ErrBusy identifies an initialization rejected by the application admission owner.
var ErrBusy = errors.New("TUI initialization is already active")

// Controller decodes SDK input without owning application state or outgoing dispatch.
type Controller struct {
	// presentation receives validated lifecycle and notification inputs.
	presentation Presentation
}

// New connects SDK input to its application consumer.
func New(presentation Presentation) *Controller {
	return &Controller{presentation: presentation}
}

// PrepareInitialize validates public startup data before application admission.
func (controller *Controller) PrepareInitialize(initialization *uiv1.Initialization) (InitializationOperation, error) {
	initial, err := DecodeInitialization(initialization)
	if err != nil {
		return nil, uisdk.Reject("INVALID_ARGUMENT", fmt.Errorf("map TUI initialization: %w", err))
	}
	operation, err := controller.presentation.PrepareInitialize(initial)
	if errors.Is(err, ErrBusy) {
		return nil, uisdk.Reject("BUSY", err)
	}
	return operation, err
}

// Notify validates one SDK notification on the terminal event loop.
func (controller *Controller) Notify(notification *uisdk.Notification) error {
	if notification == nil {
		return errors.New("UI notification is required")
	}
	input := Notification{
		Kind: NotificationConnection, OperationID: notification.OperationID(),
		Payload: mo.None[Payload](), Failure: nil,
	}
	var err error
	switch notification.Kind() {
	case uisdk.NotificationProgress:
		input.Kind = NotificationProgress
		var payload Payload
		payload, err = mapHostProgress(notification.Progress())
		input.Payload = mo.Some(payload)
	case uisdk.NotificationCompleted:
		input.Kind = NotificationCompleted
		var payload Payload
		var present bool
		payload, present, err = DecodeCompleted(notification.Completed())
		input.Payload = mo.TupleToOption(payload, present)
	case uisdk.NotificationFailed:
		input.Kind = NotificationFailed
		input.Failure = notification.OperationError()
	case uisdk.NotificationConnectionEvent:
		var payload Payload
		payload, err = DecodeConnectionEvent(notification.ConnectionEvent())
		input.Payload = mo.Some(payload)
	default:
		return errors.New("UI notification kind is unknown")
	}
	if err != nil {
		return err
	}
	return controller.presentation.Notify(input)
}

// Run enters the initialized application runtime from the SDK lifecycle.
func (controller *Controller) Run(ctx context.Context) error {
	return controller.presentation.Run(ctx)
}

// Close releases application resources when SDK initialization does not enter Run.
func (controller *Controller) Close() error {
	return controller.presentation.Close()
}
