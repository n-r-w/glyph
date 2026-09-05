package sessions

import "github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"

var _ extensioncontext.SessionState = (*Service)(nil)

// ContextSession returns one atomic identity for the active-session incarnation.
func (s *Service) ContextSession() extensioncontext.SessionIdentity {
	identity := s.contextIdentity.Load()
	if identity == nil {
		return extensioncontext.SessionIdentity{}
	}
	return *identity
}

// publishContextIdentityLocked publishes replacement identity under the active-session write lock.
func (s *Service) publishContextIdentityLocked() {
	preceding := s.ContextSession()
	s.contextIdentity.Store(&extensioncontext.SessionIdentity{
		ID: string(s.active.Header.ID), WorkingDirectory: s.workingDirectory, Incarnation: preceding.Incarnation + 1,
	})
}
