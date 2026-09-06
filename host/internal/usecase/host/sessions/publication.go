package sessions

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=publication.go -destination=publication_mock.go -package=sessions

// EntryPublisher enqueues an immutable committed entry on the active mode's ordered output.
type EntryPublisher interface {
	// PublishSessionEntry returns an acknowledgement wait without waiting under commit protection.
	PublishSessionEntry(session.Entry) (wait func(context.Context) error, err error)
}

// BindEntryPublisher installs the real mode output before extension operations start.
func (s *Service) BindEntryPublisher(publisher EntryPublisher) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.publisher = publisher
}
