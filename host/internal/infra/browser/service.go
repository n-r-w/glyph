// Package browser opens local URLs through the macOS browser service.
package browser

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
)

// Service launches one browser URL.
type Service struct{}

var _ interactions.Browser = (*Service)(nil)

// New creates a macOS browser service.
func New() *Service {
	return &Service{}
}

// Open asks macOS to open one URL and waits for the short launcher command.
func (*Service) Open(ctx context.Context, authorizationURL string) error {
	//nolint:gosec // Browser OAuth explicitly opens the provider URL.
	command := exec.CommandContext(ctx, "open", authorizationURL)
	if err := command.Run(); err != nil {
		return fmt.Errorf("open authorization URL in browser: %w", err)
	}
	return nil
}
