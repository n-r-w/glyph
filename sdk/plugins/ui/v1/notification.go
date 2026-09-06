package uiv1

import (
	"errors"

	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// NotificationKind identifies one UI state notification in Host wire order.
type NotificationKind uint8

const (
	// NotificationProgress identifies one operation progress payload.
	NotificationProgress NotificationKind = iota + 1
	// NotificationCompleted identifies one successful operation terminal payload.
	NotificationCompleted
	// NotificationFailed identifies one canceled, failed, or rejected operation terminal.
	NotificationFailed
	// NotificationConnectionEvent identifies one Host connection event.
	NotificationConnectionEvent
)

// Notification carries one validated UI state notification in Host wire order.
type Notification struct {
	// kind identifies the active notification payload.
	kind NotificationKind
	// operationID identifies an operation notification and is empty for connection events.
	operationID string
	// progress contains the active progress payload.
	progress *uiv1.HostProgress
	// completed contains the active successful terminal payload.
	completed *uiv1.HostCompleted
	// operationErr contains the active unsuccessful terminal error.
	operationErr error
	// connectionEvent contains the active connection event payload.
	connectionEvent *uiv1.HostConnectionEvent
}

// operationNotification maps one validated progress or terminal event to UI notification ownership.
func operationNotification(
	event operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted],
) (*Notification, error) {
	notification := &Notification{
		kind: 0, operationID: event.ID, progress: nil, completed: nil,
		operationErr: nil, connectionEvent: nil,
	}
	switch event.Kind {
	case operation.EventProgress:
		notification.kind = NotificationProgress
		notification.progress = event.Progress
	case operation.EventCompleted:
		notification.kind = NotificationCompleted
		notification.completed = event.Result
	case operation.EventCanceled:
		notification.kind = NotificationFailed
		notification.operationErr = newCanceledError()
	case operation.EventFailed:
		notification.kind = NotificationFailed
		notification.operationErr = newRemoteFailure(event.Code, event.Message)
	case operation.EventRejected:
		notification.kind = NotificationFailed
		notification.operationErr = newRemoteRejection(event.Code, event.Message)
	case operation.EventAccepted, operation.EventRunning:
		return nil, errors.New("accepted and running operation events are not UI state notifications")
	default:
		return nil, errors.New("unknown UI operation notification kind")
	}
	return notification, nil
}

// connectionNotification maps one validated connection event to UI notification ownership.
func connectionNotification(event *uiv1.HostConnectionEvent) *Notification {
	return &Notification{
		kind: NotificationConnectionEvent, operationID: "", progress: nil, completed: nil,
		operationErr: nil, connectionEvent: event,
	}
}

// Kind returns the closed notification kind.
func (notification *Notification) Kind() NotificationKind { return notification.kind }

// OperationID returns the operation identifier or an empty value for connection events.
func (notification *Notification) OperationID() string { return notification.operationID }

// Progress returns the progress payload when Kind is NotificationProgress.
func (notification *Notification) Progress() *uiv1.HostProgress { return notification.progress }

// Completed returns the terminal payload when Kind is NotificationCompleted.
func (notification *Notification) Completed() *uiv1.HostCompleted { return notification.completed }

// OperationError returns the terminal error when Kind is NotificationFailed.
func (notification *Notification) OperationError() error { return notification.operationErr }

// ConnectionEvent returns the connection payload when Kind is NotificationConnectionEvent.
func (notification *Notification) ConnectionEvent() *uiv1.HostConnectionEvent {
	return notification.connectionEvent
}
