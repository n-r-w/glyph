package extensioncontext

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
)

// ProtectSelectionCommit validates an issued context and coordinates session-before-runtime protection.
func (s *Service) ProtectSelectionCommit(
	ctx context.Context,
	binding modelselection.Binding,
	commit func() error,
) error {
	expected, err := s.boundSession(ctx, binding.ExtensionID, binding.RuntimeID, binding.Context)
	if err != nil {
		return err
	}
	err = s.session.ProtectContextCommit(
		ctx,
		expected,
		func() (func(), error) { return s.runtime.BeginContextCommit(binding.ExtensionID, binding.RuntimeID) },
		commit,
	)
	if err == nil {
		return nil
	}
	var bindingFailure modelselection.BindingFailure
	if errors.Is(err, session.ErrUnavailable) ||
		errors.As(err, &bindingFailure) && bindingFailure.ContextCode() == staleContextCode {
		return &ContextError{
			code: staleContextCode, cause: fmt.Errorf("protect extension selection commit: %w", err),
		}
	}
	return err
}
