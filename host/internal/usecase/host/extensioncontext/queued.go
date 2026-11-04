package extensioncontext

import (
	"context"
	"errors"
	"fmt"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// CaptureExtensionMessage captures the issued session incarnation and runtime for delayed delivery.
func (s *Service) CaptureExtensionMessage(
	ctx context.Context, extensionID, runtimeID string, reference extension.ContextRef,
	entryType, text string, visibility session.ClientVisibility,
) (session.QueuedExtensionMessage, error) {
	expected, err := s.boundSession(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return session.QueuedExtensionMessage{}, err
	}
	return session.QueuedExtensionMessage{
		Session:           expected,
		RuntimeInstanceID: runtimeID,
		Message: session.ExtensionMessage{
			ExtensionID: extensionID,
			EntryType:   entryType,
			Text:        text,
			Visibility:  visibility,
		},
	}, nil
}

// DeliverExtensionMessage persists the accepted payload while its runtime and session incarnation remain current.
func (s *Service) DeliverExtensionMessage(ctx context.Context, queued session.QueuedExtensionMessage) error {
	instance, available := s.runtime.ContextRuntime(queued.Message.ExtensionID)
	current := s.session.ContextSession()
	if !available || instance != queued.RuntimeInstanceID || current.ID != queued.Session.ID ||
		current.Incarnation != queued.Session.Incarnation {
		return nil
	}
	entry, err := s.session.AppendExtensionMessage(ctx, queued.Session, queued.Message,
		func() (func(), error) {
			return s.runtime.BeginContextCommit(queued.Message.ExtensionID, queued.RuntimeInstanceID)
		})
	if err == nil {
		return nil
	}
	// Replacement can race the initial check; the session owner and runtime guard decide at commit.
	if entry.ID == "" {
		if errors.Is(err, session.ErrUnavailable) {
			return nil
		}
		if failure, found := errors.AsType[extensioncontroller.ContextFailure](
			err,
		); found &&
			failure.ContextCode() == staleContextCode {
			return nil
		}
	}
	return fmt.Errorf("deliver queued extension message: %w", err)
}
