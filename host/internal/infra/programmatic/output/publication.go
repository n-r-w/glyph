package output

import (
	"context"
	"errors"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// PublishSelection enqueues one authoritative committed selection connection event.
func (s *Service) PublishSelection(
	selection model.Selection,
) (func(context.Context) error, error) {
	mapped, err := controller.EncodeModelSelection(selection)
	if err != nil {
		return nil, err
	}
	connection := new(programmaticv1.HostConnectionEvent)
	connection.SetModelSelectionChanged(programmaticv1.ModelSelectionChanged_builder{Selection: mapped}.Build())
	response := new(programmaticv1.OpenResponse)
	response.SetConnectionEvent(connection)
	s.mutex.Lock()
	writer := s.writer
	s.mutex.Unlock()
	if writer == nil {
		return nil, errors.New("programmatic connection writer is not active")
	}
	acknowledgement, err := writer.EnqueueAcknowledged(response, nil)
	if err != nil {
		return nil, err
	}
	return acknowledgement.Wait, nil
}

// DeliverExtensionIssue enqueues one typed nonterminal issue without an operation ID.
func (s *Service) DeliverExtensionIssue(ctx context.Context, issue lifecycle.Issue) error {
	return s.deliverExtensionIssue(ctx, issue.ExtensionID, issue.HandlerID, issue.Code, issue.Err)
}

// DeliverSelectionIssue enqueues one selection-handler issue before selection commit.
func (s *Service) DeliverSelectionIssue(ctx context.Context, issue modelselection.Issue) error {
	return s.deliverExtensionIssue(ctx, issue.ExtensionID, issue.HandlerID, issue.Code, issue.Err)
}

// deliverExtensionIssue publishes one shared public ExtensionIssue shape.
func (s *Service) deliverExtensionIssue(ctx context.Context, extensionID, handlerID, code string, cause error) error {
	connection := new(programmaticv1.HostConnectionEvent)
	connection.SetExtensionIssue(programmaticv1.ExtensionIssue_builder{
		ExtensionId: new(extensionID), HandlerId: new(handlerID),
		Code: new(code), Text: new(cause.Error()),
	}.Build())
	response := new(programmaticv1.OpenResponse)
	response.SetConnectionEvent(connection)
	s.mutex.Lock()
	writer := s.writer
	s.mutex.Unlock()
	if writer == nil {
		return errors.New("programmatic connection writer is not active")
	}
	acknowledgement, err := writer.EnqueueAcknowledged(response, nil)
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
	acknowledgement, err := writer.EnqueueAcknowledged(response, nil)
	if err != nil {
		return nil, err
	}
	return acknowledgement.Wait, nil
}
