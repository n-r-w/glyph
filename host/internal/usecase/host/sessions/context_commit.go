package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// ProtectContextCommit protects one bound state commit under session and runtime validity.
func (s *Service) ProtectContextCommit(
	ctx context.Context,
	expected contextcompaction.SessionIdentity,
	commitGuard extensioncontext.ContextCommitGuard,
	commit func() error,
) error {
	if commitGuard == nil {
		return errors.New("runtime commit validation is required")
	}
	if commit == nil {
		return errors.New("protected session commit is required")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.validateExpectedSessionLocked(ctx, expected); err != nil {
		return err
	}
	releaseRuntime, err := commitGuard()
	if err != nil {
		return fmt.Errorf("validate extension runtime before session commit: %w", err)
	}
	defer releaseRuntime()
	return commit()
}
