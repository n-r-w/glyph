package runtime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex"
)

//go:generate go tool mockgen -source=authorization.go -destination=authorization_mock.go -package=runtime

// Browser launches one authorization URL through the operating system.
type Browser interface {
	// Open waits for the short browser-launcher command, not for authorization.
	Open(ctx context.Context, authorizationURL string) error
}

var _ codex.Interaction = (*Service)(nil)

// BindBrowser connects authorization output before authentication starts.
func (s *Service) BindBrowser(browser Browser) { s.browser = browser }

// OpenBrowser performs best-effort browser output after Codex presents its authorization URL.
func (s *Service) OpenBrowser(ctx context.Context, authorizationURL string) error {
	if s.browser == nil {
		return codex.ErrInteractionUnavailable
	}
	return s.browser.Open(ctx, authorizationURL)
}
