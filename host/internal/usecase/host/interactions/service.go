// Package interactions provides Host-owned Glyph client interaction behavior.
package interactions

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex"
)

//go:generate go tool mockgen -source=service.go -destination=interfaces_mock.go -package=interactions

// ErrUnavailable identifies an interaction request with no active Glyph client.
var ErrUnavailable = errors.New("glyph client interaction is unavailable")

// Browser launches one URL through the local operating system.
type Browser interface {
	Open(ctx context.Context, authorizationURL string) error
}

// Service routes provider interaction requests to one active client and browser.
type Service struct {
	// present sends an authorization URL to the active client.
	present func(context.Context, string) error
	// browser opens authorization URLs on the local system.
	browser Browser
}

var _ codex.Interaction = (*Service)(nil)

// New creates the headless interaction service with no available Glyph client.
func New() *Service {
	return &Service{present: nil, browser: nil}
}

// NewUI creates interaction routing for one selected UI stream.
func NewUI(present func(context.Context, string) error, browser Browser) *Service {
	return &Service{present: present, browser: browser}
}

// PresentAuthorizationURL requires an active UI and synchronous stream delivery.
func (s *Service) PresentAuthorizationURL(ctx context.Context, authorizationURL string) error {
	if s.present == nil {
		return ErrUnavailable
	}
	return s.present(ctx, authorizationURL)
}

// OpenBrowser launches the local browser when UI interaction is available.
func (s *Service) OpenBrowser(ctx context.Context, authorizationURL string) error {
	if s.browser == nil {
		return ErrUnavailable
	}
	return s.browser.Open(ctx, authorizationURL)
}
