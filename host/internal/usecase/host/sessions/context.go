package sessions

import "github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"

// ContextSession returns one atomic identity for the active-session incarnation.
func (s *Service) ContextSession() contextcompaction.SessionIdentity {
	identity := s.contextIdentity.Load()
	if identity == nil {
		return contextcompaction.SessionIdentity{}
	}
	return *identity
}

// publishContextIdentityLocked publishes replacement identity under the active-session write lock.
func (s *Service) publishContextIdentityLocked() {
	preceding := s.ContextSession()
	s.contextIdentity.Store(&contextcompaction.SessionIdentity{
		ID: string(s.active.Header.ID), WorkingDirectory: s.workingDirectory, Incarnation: preceding.Incarnation + 1,
	})
}
