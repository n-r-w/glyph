package sessions

import "github.com/n-r-w/glyph/host/internal/domain/session"

// ContextSession returns one atomic identity for the active-session incarnation.
func (s *Service) ContextSession() session.Identity {
	identity := s.contextIdentity.Load()
	if identity == nil {
		return session.Identity{}
	}
	return *identity
}

// publishContextIdentityLocked publishes replacement identity under the active-session write lock.
func (s *Service) publishContextIdentityLocked() {
	preceding := s.ContextSession()
	s.contextIdentity.Store(&session.Identity{
		ID: string(s.active.Header.ID), WorkingDirectory: s.workingDirectory, Incarnation: preceding.Incarnation + 1,
	})
}
