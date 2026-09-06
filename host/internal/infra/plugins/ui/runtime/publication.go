package runtime

import (
	"context"
	"errors"
	"fmt"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

var (
	_ lifecycle.IssueDelivery = (*Service)(nil)
	_ sessions.EntryPublisher = (*Service)(nil)
)

// SetAvailability projects Host admission state to a connection event.
func (s *Service) SetAvailability(availability hostui.Availability) error {
	connection := new(uiv1.HostConnectionEvent)
	connection.SetAvailabilityChanged(uiv1.AvailabilityChanged_builder{
		Availability: new(mapAvailability(availability)),
	}.Build())
	return s.enqueueConnection(connection)
}

// ReportError publishes a classified connection failure without an operation identifier.
func (s *Service) ReportError(code, text string) error {
	if code == "" {
		return errors.New("map UI frame: error category is required")
	}
	connection := new(uiv1.HostConnectionEvent)
	connection.SetError(uiv1.Error_builder{Code: new(code), Text: new(text)}.Build())
	return s.enqueueConnection(connection)
}

// DeliverExtensionIssue publishes one observer issue through the ordered connection writer.
func (s *Service) DeliverExtensionIssue(ctx context.Context, issue lifecycle.Issue) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deliver UI extension issue: %w", err)
	}
	connection := new(uiv1.HostConnectionEvent)
	connection.SetExtensionIssue(uiv1.ExtensionIssue_builder{
		ExtensionId: new(issue.ExtensionID), HandlerId: new(issue.HandlerID),
		Code: new(issue.Code), Text: new(issue.Err.Error()),
	}.Build())
	if err := s.enqueueConnection(connection); err != nil {
		return fmt.Errorf("deliver UI extension issue: %w", err)
	}
	return nil
}

// PublishSessionEntry enqueues a committed snapshot without reading mutable session state.
func (s *Service) PublishSessionEntry(entry session.Entry) (wait func(context.Context) error, err error) {
	projected, err := controllerui.ProjectSessionTreeEntry(entry, "")
	if err != nil {
		return nil, err
	}
	mapped, err := mapSessionTreeEntry(projected)
	if err != nil {
		return nil, fmt.Errorf("map added session entry: %w", err)
	}
	connection := new(uiv1.HostConnectionEvent)
	connection.SetSessionEntryAdded(uiv1.SessionEntryAdded_builder{Entry: mapped}.Build())
	s.mutex.Lock()
	writer := s.writer
	s.mutex.Unlock()
	if writer == nil {
		return nil, errors.New("send acknowledged UI frame: operation writer is not running")
	}
	acknowledgement, err := writer.EnqueueAcknowledged(connectionRequest(connection))
	if err != nil && !errors.Is(err, operation.ErrClosed) {
		s.reportDeliveryFailure(err)
	}
	if err != nil {
		return nil, err
	}
	return acknowledgement.Wait, nil
}

// enqueueConnection uses the same writer as operation progress and terminal events.
func (s *Service) enqueueConnection(connection *uiv1.HostConnectionEvent) error {
	s.mutex.Lock()
	writer := s.writer
	s.mutex.Unlock()
	if writer == nil {
		return errors.New("send UI frame: operation writer is not running")
	}
	err := writer.Enqueue(connectionRequest(connection))
	if err != nil && !errors.Is(err, operation.ErrClosed) {
		s.reportDeliveryFailure(err)
	}
	return err
}
