package output

import (
	"context"
	"errors"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

var (
	_ lifecycle.IssueDelivery = (*Service)(nil)
	_ sessions.EntryPublisher = (*Service)(nil)
)

// DeliverExtensionIssue enqueues one typed nonterminal issue without an operation ID.
func (s *Service) DeliverExtensionIssue(ctx context.Context, issue lifecycle.Issue) error {
	connection := new(programmaticv1.HostConnectionEvent)
	connection.SetExtensionIssue(programmaticv1.ExtensionIssue_builder{
		ExtensionId: new(issue.ExtensionID), HandlerId: new(issue.HandlerID),
		Code: new(issue.Code), Text: new(issue.Err.Error()),
	}.Build())
	response := new(programmaticv1.OpenResponse)
	response.SetConnectionEvent(connection)
	s.mutex.Lock()
	writer := s.writer
	s.mutex.Unlock()
	if writer == nil {
		return errors.New("programmatic connection writer is not active")
	}
	acknowledgement, err := writer.EnqueueAcknowledged(response)
	if err != nil {
		return err
	}
	return acknowledgement.Wait(ctx)
}

// PublishSessionEntry enqueues one committed entry as a connection event without an operation ID.
func (s *Service) PublishSessionEntry(
	entry session.Entry,
) (wait func(context.Context) error, err error) {
	projected, err := controller.ProjectSessionTreeEntry(entry, "")
	if err != nil {
		return nil, err
	}
	mapped, err := controller.EncodeSessionTreeEntry(projected)
	if err != nil {
		return nil, err
	}
	connection := new(programmaticv1.HostConnectionEvent)
	connection.SetSessionEntryAdded(programmaticv1.SessionEntryAdded_builder{Entry: mapped}.Build())
	response := new(programmaticv1.OpenResponse)
	response.SetConnectionEvent(connection)
	s.mutex.Lock()
	writer := s.writer
	s.mutex.Unlock()
	if writer == nil {
		return nil, errors.New("programmatic connection writer is not active")
	}
	acknowledgement, err := writer.EnqueueAcknowledged(response)
	if err != nil {
		return nil, err
	}
	return acknowledgement.Wait, nil
}
